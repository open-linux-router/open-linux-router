package tools

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/showwin/speedtest-go/speedtest"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

func TestSpeedtestReturnsResult(t *testing.T) {
	h := &HTTP{Run: func(context.Context) (SpeedResult, error) {
		return SpeedResult{Server: "Test", Download: 123.4, Upload: 56.7}, nil
	}}
	w := httptest.NewRecorder()
	core.RouteTable(h.Routes()).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/speedtest", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var result SpeedResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Server != "Test" || result.Download != 123.4 || result.Upload != 56.7 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestSpeedtestRejectsConcurrentRun(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	h := &HTTP{Run: func(context.Context) (SpeedResult, error) {
		close(started)
		<-release
		return SpeedResult{}, nil
	}}
	routes := core.RouteTable(h.Routes())
	done := make(chan struct{})
	go func() {
		defer close(done)
		routes.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/speedtest", nil))
	}()
	<-started
	w := httptest.NewRecorder()
	routes.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/speedtest", nil))
	close(release)
	<-done
	if w.Code != http.StatusConflict {
		t.Fatalf("concurrent request returned %d, want 409", w.Code)
	}
}

func TestTryServersFallsBackAfterUnusableTransfer(t *testing.T) {
	servers := speedtest.Servers{
		{Sponsor: "First", Name: "Near"},
		{Sponsor: "Second", Name: "Next"},
	}
	var tried []string
	result, err := tryServers(context.Background(), servers, func(_ context.Context, server *speedtest.Server) (SpeedResult, error) {
		tried = append(tried, server.Sponsor)
		if server == servers[0] {
			return SpeedResult{}, errors.New("download returned no usable data")
		}
		return SpeedResult{Server: server.Sponsor, Download: 100}, nil
	})
	if err != nil || result.Server != "Second" || len(tried) != 2 {
		t.Fatalf("result=%+v, tried=%v, err=%v", result, tried, err)
	}
}

func TestTryServersLimitsAttemptsAndReportsFailure(t *testing.T) {
	servers := speedtest.Servers{
		{Sponsor: "One", Name: "A"}, {Sponsor: "Two", Name: "B"},
		{Sponsor: "Three", Name: "C"}, {Sponsor: "Four", Name: "D"},
	}
	attempts := 0
	_, err := tryServers(context.Background(), servers, func(context.Context, *speedtest.Server) (SpeedResult, error) {
		attempts++
		return SpeedResult{}, errors.New("upload returned no usable data")
	})
	if attempts != 3 || err == nil || !strings.Contains(err.Error(), "Three (C): upload returned no usable data") {
		t.Fatalf("attempts=%d, err=%v", attempts, err)
	}
}
