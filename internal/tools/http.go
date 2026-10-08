// Package tools provides on-demand diagnostics run from the router itself.
package tools

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/showwin/speedtest-go/speedtest"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

const ModuleName = "tools"

type SpeedResult struct {
	Server   string  `json:"server"`
	Location string  `json:"location"`
	Latency  float64 `json:"latency_ms"`
	Jitter   float64 `json:"jitter_ms"`
	Download float64 `json:"download_mbps"`
	Upload   float64 `json:"upload_mbps"`
}

type HTTP struct {
	running     atomic.Bool
	pingRunning atomic.Bool
	tracing     atomic.Bool
	Trace       func(context.Context, string) (TraceResult, error)
	// RunPing is replaceable in tests; nil sends real ICMP echo requests.
	RunPing func(context.Context, string) (PingResult, error)
	// Run is replaceable in tests; nil uses the actual network test.
	Run func(context.Context) (SpeedResult, error)
}

func (h *HTTP) Routes() []core.Route {
	return []core.Route{{
		Method: "POST", Path: "/speedtest", Mutating: true,
		Summary: "Measure this router's internet latency, download and upload speed using speedtest-go; consumes bandwidth.",
		Handler: h.speedtest,
	}, {
		Method: "POST", Path: "/ping", Mutating: true,
		Summary: "Send four ICMP echo requests from this router to a hostname or IP address and report packet loss and latency.",
		Handler: h.ping,
	}, {
		Method: "POST", Path: "/traceroute", Mutating: true,
		Summary: "Trace the route from this router to a domain or IP using nexttrace; sends network probes.",
		Handler: h.traceroute,
	}}
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

	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	run := h.Run
	if run == nil {
		run = measure
	}
	result, err := run(ctx)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		core.WriteError(w, http.StatusServiceUnavailable, "speed test failed: "+err.Error())
		return
	}
	core.WriteJSON(w, http.StatusOK, result)
}

func measure(ctx context.Context) (SpeedResult, error) {
	client := speedtest.New()
	// Bound transfer time and concurrency; a test should not saturate the router indefinitely.
	client.SetCaptureTime(10 * time.Second).SetNThread(4)
	servers, err := client.FetchServerListContext(ctx)
	if err != nil {
		return SpeedResult{}, fmt.Errorf("finding test servers: %w", err)
	}
	available := *servers.Available()
	if len(available) == 0 {
		return SpeedResult{}, errors.New("no reachable test server available")
	}
	return tryServers(ctx, available, func(ctx context.Context, server *speedtest.Server) (SpeedResult, error) {
		// Each attempt needs fresh counters and workers, even when the previous
		// server answered ping but failed to transfer data.
		client.Reset()
		return testServer(ctx, server)
	})
}

// A server that answers ping is not necessarily able to serve the test files
// or accept uploads. Try a few alternatives without turning one click into an
// unbounded series of bandwidth-heavy tests.
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
		Download: server.DLSpeed.Mbps(),
		Upload:   server.ULSpeed.Mbps(),
	}, nil
}

func firstError(err, fallback error) error {
	if err != nil {
		return err
	}
	return fallback
}
