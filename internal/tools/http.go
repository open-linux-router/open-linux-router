// Package tools provides on-demand diagnostics run from the router itself.
package tools

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/showwin/speedtest-go/speedtest"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

const ModuleName = "tools"

type SpeedResult struct {
	Server   string  `json:"server"`
	Location string  `json:"location"`
	Exit     string  `json:"exit,omitempty"`
	Latency  float64 `json:"latency_ms"`
	Jitter   float64 `json:"jitter_ms"`
	Download float64 `json:"download_mbps"`
	Upload   float64 `json:"upload_mbps"`
}

type ServerOption struct {
	ID       string  `json:"id"`
	Sponsor  string  `json:"sponsor"`
	Location string  `json:"location"`
	Latency  float64 `json:"latency_ms"`
}

type HTTP struct {
	running     atomic.Bool
	pingRunning atomic.Bool
	tracing     atomic.Bool
	natRunning  atomic.Bool

	Trace  func(context.Context, string) (TraceResult, error)
	RunNAT func(context.Context) (NATResult, error)
	// RunPing is replaceable in tests; nil sends real ICMP echo requests.
	RunPing      func(context.Context, string) (PingResult, error)
	LANAddresses func() ([]string, error)
	StartIperf   func(string, int) (func(), <-chan struct{}, error)
	lanMu        sync.Mutex
	lan          *lanSession
	// ResolveExit returns a current fwmark, rejecting disabled or blocked exits.
	ResolveExit func(string) (uint32, error)
	// Run is replaceable in tests; nil uses the actual network test.
	Run func(context.Context) (SpeedResult, error)
}

func (h *HTTP) Routes() []core.Route {
	exit := core.QueryParam{Name: "exit", Type: "string", Summary: "Configured way out; empty uses the router's own default route."}
	return []core.Route{
		{Method: "GET", Path: "/speedtest/servers", Tool: "show speedtest servers", Summary: "Find nearby speed test servers through the selected way out; makes network requests but transfers no test data.", Query: []core.QueryParam{exit}, Handler: h.servers},
		{Method: "POST", Path: "/speedtest", Mutating: true, Summary: "Measure this router's internet latency, download and upload speed using speedtest-go; consumes bandwidth.", Query: []core.QueryParam{exit, {Name: "server_id", Type: "string", Summary: "Speed test server ID from the server list; empty picks a reachable server automatically."}}, Handler: h.speedtest},
		{Method: "POST", Path: "/nat", Mutating: true, Summary: "Probe this router’s default-route UDP mapping and filtering through public STUN servers; sends UDP packets.", Handler: h.nat},
		{Method: "GET", Path: "/lan-test", Tool: "show lan test", Summary: "List LAN addresses and the current temporary iperf3 server session.", Handler: h.lanStatus},
		{Method: "POST", Path: "/lan-test", Mutating: true, Summary: "Start a temporary iperf3 server bound to a configured LAN address; consumes router resources when a client connects.", Handler: h.startLAN},
		{Method: "DELETE", Path: "/lan-test", Mutating: true, Summary: "Stop the temporary iperf3 server.", Handler: h.stopLAN},
		{Method: "POST", Path: "/ping", Mutating: true, Summary: "Send four ICMP echo requests from this router to a hostname or IP address and report packet loss and latency.", Handler: h.ping},
		{Method: "POST", Path: "/traceroute", Mutating: true, Summary: "Trace the route from this router to a domain or IP using nexttrace; sends network probes.", Handler: h.traceroute},
	}
}

func (h *HTTP) client(exit string) (*speedtest.Speedtest, error) {
	config := &speedtest.UserConfig{}
	if exit != "" {
		if h.ResolveExit == nil {
			return nil, errors.New("exit selection is unavailable")
		}
		mark, err := h.ResolveExit(exit)
		if err != nil {
			return nil, err
		}
		config.DialerControl = markSocket(mark)
	}
	client := speedtest.New(speedtest.WithUserConfig(config))
	// Environment proxy settings would send the transfer through the proxy's
	// route instead of the router default or the requested way out.
	config.T.Proxy = nil
	return client, nil
}

func (h *HTTP) servers(w http.ResponseWriter, r *http.Request) {
	client, err := h.client(r.URL.Query().Get("exit"))
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	servers, err := client.FetchServerListContext(ctx)
	if err != nil {
		core.WriteError(w, http.StatusServiceUnavailable, "finding test servers: "+err.Error())
		return
	}
	options := make([]ServerOption, 0, 20)
	for _, server := range *servers.Available() {
		if server.ID == "" {
			continue
		}
		options = append(options, ServerOption{ID: server.ID, Sponsor: server.Sponsor, Location: server.Name + ", " + server.Country, Latency: float64(server.Latency) / float64(time.Millisecond)})
		if len(options) == 20 {
			break
		}
	}
	core.WriteJSON(w, http.StatusOK, map[string]any{"servers": options})
}

func (h *HTTP) traceroute(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Target string `json:"target"`
	}
	if err := core.DecodeJSON(w, r, &input); err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	target := input.Target
	if !validTraceTarget(target) {
		core.WriteError(w, http.StatusUnprocessableEntity, "enter a valid domain name or IP address")
		return
	}
	if !h.tracing.CompareAndSwap(false, true) {
		core.WriteError(w, http.StatusConflict, "a traceroute is already running")
		return
	}
	defer h.tracing.Store(false)
	ctx, cancel := context.WithTimeout(r.Context(), traceTimeout)
	defer cancel()
	run := h.Trace
	if run == nil {
		run = runTrace
	}
	result, err := run(ctx, target)
	if r.Context().Err() != nil {
		return
	}
	if err != nil {
		core.WriteError(w, http.StatusServiceUnavailable, "traceroute failed: "+err.Error())
		return
	}
	core.WriteJSON(w, http.StatusOK, result)
}

func (h *HTTP) speedtest(w http.ResponseWriter, r *http.Request) {
	if !h.running.CompareAndSwap(false, true) {
		core.WriteError(w, http.StatusConflict, "a speed test is already running")
		return
	}
	defer h.running.Store(false)

	exit, id := r.URL.Query().Get("exit"), r.URL.Query().Get("server_id")
	if len(id) > 32 || strings.ContainsAny(id, " \t\r\n") {
		core.WriteError(w, http.StatusBadRequest, "invalid server ID")
		return
	}
	client, err := h.client(exit)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	run := h.Run
	if run == nil {
		run = func(ctx context.Context) (SpeedResult, error) { return measure(ctx, client, id) }
	}
	result, err := run(ctx)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		core.WriteError(w, http.StatusServiceUnavailable, "speed test failed: "+err.Error())
		return
	}
	result.Exit = exit
	core.WriteJSON(w, http.StatusOK, result)
}

func measure(ctx context.Context, client *speedtest.Speedtest, id string) (SpeedResult, error) {
	client.SetCaptureTime(10 * time.Second).SetNThread(4)
	servers, err := client.FetchServerListContext(ctx)
	if err != nil {
		return SpeedResult{}, fmt.Errorf("finding test servers: %w", err)
	}
	available := *servers.Available()
	if len(available) == 0 {
		return SpeedResult{}, errors.New("no reachable test server available")
	}
	if id != "" {
		server, err := serverByID(available, id)
		if err != nil {
			return SpeedResult{}, err
		}
		client.Reset()
		return testServer(ctx, server)
	}
	return tryServers(ctx, available, func(ctx context.Context, server *speedtest.Server) (SpeedResult, error) {
		client.Reset()
		return testServer(ctx, server)
	})
}

func serverByID(servers speedtest.Servers, id string) (*speedtest.Server, error) {
	for _, server := range servers {
		if server.ID == id {
			return server, nil
		}
	}
	return nil, fmt.Errorf("selected server %q is no longer available; refresh the list", id)
}

func tryServers(ctx context.Context, servers speedtest.Servers, run func(context.Context, *speedtest.Server) (SpeedResult, error)) (SpeedResult, error) {
	var lastErr error
	for i, server := range servers {
		if i == 3 || ctx.Err() != nil {
			break
		}
		result, err := run(ctx, server)
		if err == nil {
			return result, nil
		}
		lastErr = fmt.Errorf("%s (%s): %w", server.Sponsor, server.Name, err)
	}
	if ctx.Err() != nil {
		return SpeedResult{}, ctx.Err()
	}
	if lastErr == nil {
		return SpeedResult{}, errors.New("no test server available")
	}
	return SpeedResult{}, fmt.Errorf("no usable test server among the first three candidates; last error: %w", lastErr)
}

func testServer(ctx context.Context, server *speedtest.Server) (SpeedResult, error) {
	if err := server.PingTestContext(ctx, nil); err != nil {
		return SpeedResult{}, fmt.Errorf("measuring latency: %w", err)
	}
	if err := server.DownloadTestContext(ctx); err != nil || ctx.Err() != nil {
		return SpeedResult{}, fmt.Errorf("measuring download: %w", firstError(err, ctx.Err()))
	}
	if server.DLSpeed <= 0 {
		return SpeedResult{}, errors.New("download returned no usable data")
	}
	if err := server.UploadTestContext(ctx); err != nil || ctx.Err() != nil {
		return SpeedResult{}, fmt.Errorf("measuring upload: %w", firstError(err, ctx.Err()))
	}
	if server.ULSpeed <= 0 {
		return SpeedResult{}, errors.New("upload returned no usable data")
	}
	return SpeedResult{
		Server: server.Sponsor, Location: server.Name + ", " + server.Country,
		Latency:  float64(server.Latency) / float64(time.Millisecond),
		Jitter:   float64(server.Jitter) / float64(time.Millisecond),
		Download: server.DLSpeed.Mbps(), Upload: server.ULSpeed.Mbps(),
	}, nil
}

func firstError(err, fallback error) error {
	if err != nil {
		return err
	}
	return fallback
}

func (h *HTTP) nat(w http.ResponseWriter, r *http.Request) {
	if !h.natRunning.CompareAndSwap(false, true) {
		core.WriteError(w, http.StatusConflict, "a NAT test is already running")
		return
	}
	defer h.natRunning.Store(false)
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	run := h.RunNAT
	if run == nil {
		run = runNAT
	}
	result, err := run(ctx)
	if r.Context().Err() != nil {
		return
	}
	if err != nil {
		core.WriteError(w, http.StatusServiceUnavailable, "NAT test failed: "+err.Error())
		return
	}
	core.WriteJSON(w, http.StatusOK, result)
}
