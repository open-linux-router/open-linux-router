// Package tools provides on-demand diagnostics run from the router itself.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/showwin/speedtest-go/speedtest"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

const ModuleName = "tools"

// serverListLimit caps how many servers the picker returns: enough for a real
// choice, short enough to stay a list rather than a directory.
const serverListLimit = 50

// serverListURL is Ookla's nearby-server payload. One request answers with the
// servers this caller's way out would use, ordered by distance and carrying no
// latency, so opening the picker costs a round trip rather than a ping sweep.
const serverListURL = "https://www.speedtest.net/api/js/servers"

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
	ID       string `json:"id"`
	Sponsor  string `json:"sponsor"`
	Location string `json:"location"`
}

// ServerList reports the servers the provider lists for one way out, and how
// many it listed before the picker's limit, so a truncated list explains itself.
type ServerList struct {
	Servers []ServerOption `json:"servers"`
	Total   int            `json:"total"`
}

// providerServer is one row of Ookla's nearby-server payload, narrowed to the
// fields the picker shows.
type providerServer struct {
	ID      string `json:"id"`
	Sponsor string `json:"sponsor"`
	Name    string `json:"name"`
	Country string `json:"country"`
}

type HTTP struct {
	running     atomic.Bool
	pingRunning atomic.Bool
	tracing     atomic.Bool
	natRunning  atomic.Bool
	quality     qualityRun
	// QualityContext follows daemon shutdown; nil is suitable for standalone use.
	QualityContext context.Context
	// QualityPath stores the latest finished assessment; empty disables persistence.
	QualityPath  string
	qualityProbe func(context.Context, qualityTarget, string) QualitySample

	Trace  func(context.Context, string) (TraceResult, error)
	RunNAT func(context.Context) (NATResult, error)
	// RunPing is replaceable in tests; nil sends real ICMP echo requests.
	RunPing      func(context.Context, string) (PingResult, error)
	LANAddresses func() ([]string, error)
	LANProbe     func(string) (string, error)
	StartIperf   func(string, int) (func(), <-chan struct{}, error)
	lanMu        sync.Mutex
	lan          *lanSession
	linkMu       sync.Mutex
	linkViewers  []linkTestViewer
	// ResolveExit returns a current fwmark, rejecting disabled or blocked exits.
	ResolveExit func(string) (uint32, error)
	// Run is replaceable in tests; nil uses the actual network test.
	Run func(context.Context) (SpeedResult, error)
	// ListServers is replaceable in tests; nil queries Ookla through the
	// selected way out.
	ListServers func(context.Context, string, string) (ServerList, error)
}

func (h *HTTP) Routes() []core.Route {
	exit := core.QueryParam{Name: "exit", Type: "string", Summary: "Configured way out; empty uses the router's own default route."}
	search := core.QueryParam{Name: "search", Type: "string", Summary: "Optional word to widen the server list, matched by Ookla against provider, city and country names; empty lists nearby servers."}
	return []core.Route{
		{Method: "GET", Path: "/quality", Tool: "show quality", Summary: "Read the latest network quality assessment, evidence and score coverage without starting tests.", Handler: h.getQuality},
		{Method: "POST", Path: "/quality", Mutating: true, Summary: "Run a full router default-route quality assessment, or rerun one scope and update its scores; the reference transfer consumes bandwidth.", Query: []core.QueryParam{{Name: "scope", Type: "string", Summary: "all (default) runs every test; responses, target, speed and nat rerun one part and keep the rest."}, {Name: "target", Type: "string", Summary: "Service target ID when scope is target: cloudflare, google or github."}, {Name: "speed", Type: "boolean", Summary: "Include the bandwidth-consuming reference transfer with scope=all; defaults to false."}}, Handler: h.startQuality},
		{Method: "DELETE", Path: "/quality", Mutating: true, Summary: "Cancel the active network quality assessment and retain its completed evidence.", Handler: h.cancelQuality},
		{Method: "GET", Path: "/speedtest/servers", Tool: "show speedtest servers", Summary: "List the speed test servers near the selected way out; one network request, no latency probing and no test data.", Query: []core.QueryParam{exit, search}, Handler: h.servers},
		{Method: "POST", Path: "/speedtest", Mutating: true, Summary: "Measure this router's internet latency, download and upload speed using speedtest-go; consumes bandwidth.", Query: []core.QueryParam{exit, search, {Name: "server_id", Type: "string", Summary: "Speed test server ID from the server list; empty probes the nearby servers and starts with the first that answers."}}, Handler: h.speedtest},
		{Method: "POST", Path: "/nat", Mutating: true, Summary: "Probe this router’s default-route UDP mapping and filtering through public STUN servers; sends UDP packets.", Handler: h.nat},
		{Method: "GET", Path: "/lan-test", Tool: "show lan test", Summary: "List LAN addresses and the current temporary iperf3 server session.", Handler: h.lanStatus},
		{Method: "POST", Path: "/lan-test", Mutating: true, Summary: "Start a temporary iperf3 server bound to a configured LAN address; consumes router resources when a client connects.", Handler: h.startLAN},
		{Method: "DELETE", Path: "/lan-test", Mutating: true, Summary: "Stop the temporary iperf3 server.", Handler: h.stopLAN},
		{Method: "POST", Path: "/link-test/session", Mutating: true, Summary: "Authorize this browser to run the embedded LibreSpeed device-to-router test; the cookie it sets is not an API credential.", Handler: h.linkTestSession},
		{Method: "POST", Path: "/ping", Mutating: true, Summary: "Send four ICMP echo requests from this router to a hostname or IP address and report packet loss and latency.", Handler: h.ping},
		{Method: "POST", Path: "/traceroute", Mutating: true, Summary: "Trace the route from this router to a domain or IP using nexttrace; sends network probes.", Handler: h.traceroute},
	}
}

// exitError marks what the router itself rejected, such as a way out that is
// gone, so a handler answers 400 instead of blaming the network.
type exitError struct{ err error }

func (e exitError) Error() string { return e.err.Error() }
func (e exitError) Unwrap() error { return e.err }

func (h *HTTP) client(exit, search string) (*speedtest.Speedtest, error) {
	config := &speedtest.UserConfig{Keyword: search}
	if exit != "" {
		if h.ResolveExit == nil {
			return nil, exitError{errors.New("exit selection is unavailable")}
		}
		mark, err := h.ResolveExit(exit)
		if err != nil {
			return nil, exitError{err}
		}
		config.DialerControl = markSocket(mark)
	}
	client := speedtest.New(speedtest.WithUserConfig(config))
	// Environment proxy settings would send the transfer through the proxy's
	// route instead of the router default or the requested way out.
	config.T.Proxy = nil
	return client, nil
}

// serverSearch bounds the optional search term. Ookla matches it against
// provider and place names, so it is free text rather than an identifier.
func serverSearch(raw string) (string, error) {
	search := strings.TrimSpace(raw)
	if len(search) > 64 {
		return "", errors.New("server search must be 64 characters or fewer")
	}
	if strings.ContainsFunc(search, unicode.IsControl) {
		return "", errors.New("server search contains control characters")
	}
	return search, nil
}

func (h *HTTP) servers(w http.ResponseWriter, r *http.Request) {
	exit := r.URL.Query().Get("exit")
	search, err := serverSearch(r.URL.Query().Get("search"))
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	find := h.ListServers
	if find == nil {
		find = h.listServers
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	list, err := find(ctx, exit, search)
	if err != nil {
		var badExit exitError
		if errors.As(err, &badExit) {
			core.WriteError(w, http.StatusBadRequest, badExit.Error())
			return
		}
		core.WriteError(w, http.StatusServiceUnavailable, "finding test servers: "+err.Error())
		return
	}
	core.WriteJSON(w, http.StatusOK, list)
}

func (h *HTTP) listServers(ctx context.Context, exit, search string) (ServerList, error) {
	client, err := h.client(exit, search)
	if err != nil {
		return ServerList{}, err
	}
	// The speedtest client is the round tripper, so this request carries the
	// selected way out and its user agent, and the list is the one that way out
	// would be offered.
	rows, err := providerServers(ctx, &http.Client{Transport: client}, serverListURL, search)
	if err != nil {
		return ServerList{}, err
	}
	return serverOptions(rows), nil
}

// providerServers asks the provider for its nearby list and returns the rows the
// picker needs.
func providerServers(ctx context.Context, doer *http.Client, endpoint, search string) ([]providerServer, error) {
	target, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	query := target.Query()
	if search != "" {
		query.Set("search", search)
	}
	target.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := doer.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("provider answered %s", resp.Status)
	}
	var rows []providerServer
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		return nil, fmt.Errorf("reading provider response: %w", err)
	}
	return rows, nil
}

// serverOptions keeps the provider's distance order, drops rows the router
// could not test by ID, stops at the picker's limit, and reports how many the
// provider listed so a truncated list explains itself.
func serverOptions(rows []providerServer) ServerList {
	total := 0
	options := make([]ServerOption, 0, min(len(rows), serverListLimit))
	for _, row := range rows {
		if row.ID == "" {
			continue
		}
		total++
		if len(options) < serverListLimit {
			options = append(options, ServerOption{ID: row.ID, Sponsor: row.Sponsor, Location: place(row.Name, row.Country)})
		}
	}
	return ServerList{Servers: options, Total: total}
}

// place joins the two labels Ookla splits a server's site into, without leaving
// a dangling separator when one of them is missing or repeats the other.
func place(name, country string) string {
	switch {
	case name == "":
		return country
	case country == "" || country == name:
		return name
	}
	return name + ", " + country
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
	search, err := serverSearch(r.URL.Query().Get("search"))
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	client, err := h.client(exit, search)
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
	if id != "" {
		// The picker already knows which server the user wants; asking Ookla for
		// that one server keeps the chosen path free of a ping sweep over the
		// rest, and still catches an ID the provider has retired.
		server, err := client.FetchServerByIDContext(ctx, id)
		if errors.Is(err, speedtest.ErrServerNotFound) {
			return SpeedResult{}, fmt.Errorf("selected server %q is no longer available; refresh the list", id)
		}
		if err != nil {
			return SpeedResult{}, fmt.Errorf("finding test server %s: %w", id, err)
		}
		client.Reset()
		return testServer(ctx, server)
	}
	servers, err := client.FetchServerListContext(ctx)
	if err != nil {
		return SpeedResult{}, fmt.Errorf("finding test servers: %w", err)
	}
	available := *servers.Available()
	if len(available) == 0 {
		return SpeedResult{}, errors.New("no reachable test server available")
	}
	return tryServers(ctx, available, func(ctx context.Context, server *speedtest.Server) (SpeedResult, error) {
		client.Reset()
		return testServer(ctx, server)
	})
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
		Server: server.Sponsor, Location: place(server.Name, server.Country),
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
