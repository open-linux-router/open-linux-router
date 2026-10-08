package tools

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	probing "github.com/prometheus-community/pro-bing"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

type PingResult struct {
	Target      string  `json:"target"`
	Address     string  `json:"address"`
	Sent        int     `json:"sent"`
	Received    int     `json:"received"`
	LossPercent float64 `json:"loss_percent"`
	MinMS       float64 `json:"min_ms"`
	AvgMS       float64 `json:"avg_ms"`
	MaxMS       float64 `json:"max_ms"`
}

func validPingTarget(target string) bool {
	if net.ParseIP(target) != nil {
		return true
	}
	if len(target) > 253 || target == "" || strings.HasSuffix(target, ".") {
		return false
	}
	for _, label := range strings.Split(target, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if c != '-' && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
				return false
			}
		}
	}
	return true
}

func (h *HTTP) ping(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Target string `json:"target"`
	}
	if err := core.DecodeJSON(w, r, &request); err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	request.Target = strings.TrimSpace(request.Target)
	if !validPingTarget(request.Target) {
		core.WriteError(w, http.StatusBadRequest, "target must be an IP address or hostname")
		return
	}
	if !h.pingRunning.CompareAndSwap(false, true) {
		core.WriteError(w, http.StatusConflict, "a ping is already running")
		return
	}
	defer h.pingRunning.Store(false)

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	run := h.RunPing
	if run == nil {
		run = measurePing
	}
	result, err := run(ctx, request.Target)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		core.WriteError(w, http.StatusServiceUnavailable, "ping failed: "+err.Error())
		return
	}
	core.WriteJSON(w, http.StatusOK, result)
}

func measurePing(ctx context.Context, target string) (PingResult, error) {
	pinger := probing.New(target)
	pinger.ResolveTimeout = 5 * time.Second
	pinger.Count = 4
	pinger.Interval = time.Second
	pinger.Timeout = 8 * time.Second
	if err := pinger.RunWithContext(ctx); err != nil {
		return PingResult{}, fmt.Errorf("probing %s: %w", target, err)
	}
	if err := ctx.Err(); err != nil {
		return PingResult{}, err
	}
	stats := pinger.Statistics()
	if stats == nil || stats.IPAddr == nil {
		return PingResult{}, errors.New("no ping statistics available")
	}
	return PingResult{
		Target: target, Address: stats.IPAddr.String(),
		Sent: stats.PacketsSent, Received: stats.PacketsRecv, LossPercent: stats.PacketLoss,
		MinMS: ms(stats.MinRtt), AvgMS: ms(stats.AvgRtt), MaxMS: ms(stats.MaxRtt),
	}, nil
}

func ms(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }
