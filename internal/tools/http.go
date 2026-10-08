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
	running atomic.Bool
	// Run is replaceable in tests; nil uses the actual network test.
	Run func(context.Context) (SpeedResult, error)
}

func (h *HTTP) Routes() []core.Route {
	return []core.Route{{
		Method: "POST", Path: "/speedtest", Mutating: true,
		Summary: "Measure this router's internet latency, download and upload speed using speedtest-go; consumes bandwidth.",
		Handler: h.speedtest,
	}}
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
	selected, err := (*servers.Available()).FindServer(nil)
	if err != nil || len(selected) == 0 {
		return SpeedResult{}, errors.New("no test server available")
	}
	server := selected[0]
	if err := server.PingTestContext(ctx, nil); err != nil {
		return SpeedResult{}, fmt.Errorf("measuring latency: %w", err)
	}
	if err := server.DownloadTestContext(ctx); err != nil || ctx.Err() != nil {
		return SpeedResult{}, fmt.Errorf("measuring download: %w", firstError(err, ctx.Err()))
	}
	if err := server.UploadTestContext(ctx); err != nil || ctx.Err() != nil {
		return SpeedResult{}, fmt.Errorf("measuring upload: %w", firstError(err, ctx.Err()))
	}
	if server.DLSpeed < 0 || server.ULSpeed < 0 {
		return SpeedResult{}, errors.New("test server did not return a valid transfer rate")
	}
	return SpeedResult{
		Server: server.Sponsor, Location: server.Name + ", " + server.Country,
		Latency:  float64(server.Latency) / float64(time.Millisecond),
		Jitter:   float64(server.Jitter) / float64(time.Millisecond),
		Download: float64(server.DLSpeed) * 8 / 1e6,
		Upload:   float64(server.ULSpeed) * 8 / 1e6,
	}, nil
}

func firstError(err, fallback error) error {
	if err != nil {
		return err
	}
	return fallback
}
