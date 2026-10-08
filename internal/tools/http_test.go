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

func TestPingReturnsResult(t *testing.T) {
	h := &HTTP{RunPing: func(_ context.Context, target string) (PingResult, error) {
		if target != "example.com" {
			t.Errorf("target = %q", target)
		}
		return PingResult{Target: target, Address: "192.0.2.1", Sent: 4, Received: 0, LossPercent: 100}, nil
	}}
	w := httptest.NewRecorder()
	core.RouteTable(h.Routes()).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/ping", strings.NewReader(`{"target":"example.com"}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var result PingResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Address != "192.0.2.1" || result.LossPercent != 100 {
		t.Fatalf("result = %+v", result)
	}
}

func TestPingRejectsInvalidTargets(t *testing.T) {
	h := &HTTP{RunPing: func(context.Context, string) (PingResult, error) {
		t.Fatal("ran ping with invalid target")
		return PingResult{}, nil
	}}
	for _, target := range []string{"", "https://example.com", "example.com:80", "-bad.example", "a..b", "host/name", "fe80::1%eth0"} {
		t.Run(target, func(t *testing.T) {
			body, _ := json.Marshal(map[string]string{"target": target})
			w := httptest.NewRecorder()
			core.RouteTable(h.Routes()).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/ping", strings.NewReader(string(body))))
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
		})
	}
	for _, target := range []string{"192.0.2.1", "2001:db8::1", "router.local"} {
		if !validPingTarget(target) {
			t.Errorf("valid target %q rejected", target)
		}
	}
}

func TestPingRejectsConcurrentRun(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	h := &HTTP{RunPing: func(context.Context, string) (PingResult, error) {
		close(started)
		<-release
		return PingResult{}, nil
	}}
	routes := core.RouteTable(h.Routes())
	done := make(chan struct{})
	go func() {
		defer close(done)
		routes.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/ping", strings.NewReader(`{"target":"localhost"}`)))
	}()
	<-started
	w := httptest.NewRecorder()
	routes.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/ping", strings.NewReader(`{"target":"localhost"}`)))
	close(release)
	<-done
	if w.Code != http.StatusConflict {
		t.Fatalf("status %d, want 409", w.Code)
	}
}

func TestTracerouteReturnsResult(t *testing.T) {
	h := &HTTP{Trace: func(_ context.Context, target string) (TraceResult, error) {
		return TraceResult{Target: target, Output: "1  192.0.2.1  1ms\n", MapURL: "https://api.nxtrace.org/tracemap/html/id.html"}, nil
	}}
	w := httptest.NewRecorder()
	core.RouteTable(h.Routes()).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/traceroute", strings.NewReader(`{"target":"example.com"}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var result TraceResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Target != "example.com" || !strings.Contains(result.Output, "192.0.2.1") || result.MapURL == "" {
		t.Fatalf("result: %+v", result)
	}
}

func TestTracerouteRejectsInvalidTargets(t *testing.T) {
	for _, target := range []string{"", "-version", "foo bar", "https://example.com", "example.com\n--output", "a..b", "foo_bar"} {
		t.Run(target, func(t *testing.T) {
			h := &HTTP{Trace: func(context.Context, string) (TraceResult, error) { t.Fatal("ran trace"); return TraceResult{}, nil }}
			w := httptest.NewRecorder()
			body, _ := json.Marshal(map[string]string{"target": target})
			core.RouteTable(h.Routes()).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/traceroute", strings.NewReader(string(body))))
			if w.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestTracerouteRejectsConcurrentRun(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	h := &HTTP{Trace: func(context.Context, string) (TraceResult, error) {
		close(started)
		<-release
		return TraceResult{}, nil
	}}
	routes := core.RouteTable(h.Routes())
	request := func() *http.Request {
		return httptest.NewRequest(http.MethodPost, "/traceroute", strings.NewReader(`{"target":"1.1.1.1"}`))
	}
	done := make(chan struct{})
	go func() { defer close(done); routes.ServeHTTP(httptest.NewRecorder(), request()) }()
	<-started
	w := httptest.NewRecorder()
	routes.ServeHTTP(w, request())
	close(release)
	<-done
	if w.Code != http.StatusConflict {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
}

func TestLimitedTraceOutput(t *testing.T) {
	var b limitedBuffer
	data := strings.Repeat("x", traceOutputLimit+100)
	if n, err := b.Write([]byte(data)); n != len(data) || err != nil || b.Len() != traceOutputLimit || !b.truncated {
		t.Fatalf("write=%d, length=%d, truncated=%v, err=%v", n, b.Len(), b.truncated, err)
	}
}
