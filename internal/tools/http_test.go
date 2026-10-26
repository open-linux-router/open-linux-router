package tools

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

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
func TestSelectedServerMustBeReachable(t *testing.T) {
	servers := speedtest.Servers{{ID: "1", Sponsor: "Near"}, {ID: "2", Sponsor: "Far"}}
	server, err := serverByID(servers, "2")
	if err != nil || server != servers[1] {
		t.Fatalf("server=%v, err=%v", server, err)
	}
	if _, err := serverByID(servers, "3"); err == nil {
		t.Fatal("unlisted server was accepted")
	}
}

func TestSelectedExitIsValidatedForBothRoutes(t *testing.T) {
	h := &HTTP{ResolveExit: func(name string) (uint32, error) {
		if name != "working" {
			return 0, errors.New("way out unavailable")
		}
		return 0x10000, nil
	}}
	routes := core.RouteTable(h.Routes())
	for _, path := range []string{"/speedtest?exit=missing", "/speedtest/servers?exit=missing"} {
		method := http.MethodPost
		if strings.Contains(path, "servers") {
			method = http.MethodGet
		}
		w := httptest.NewRecorder()
		routes.ServeHTTP(w, httptest.NewRequest(method, path, nil))
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400", path, w.Code)
		}
	}
}

func TestServerOptionsDropUnreachableAndCountBoth(t *testing.T) {
	servers := speedtest.Servers{
		{ID: "1", Sponsor: "Near", Name: "City", Country: "Country A", Latency: 12 * time.Millisecond},
		{ID: "2", Sponsor: "Gone", Name: "City", Country: "Country B", Latency: speedtest.PingTimeout},
		{ID: "3", Sponsor: "Far", Name: "Town", Country: "Country C", Latency: 90 * time.Millisecond},
	}
	list := serverOptions(servers)
	if list.Candidates != 3 || list.Reachable != 2 || len(list.Servers) != 2 {
		t.Fatalf("list = %+v", list)
	}
	if list.Servers[0].ID != "1" || list.Servers[0].Latency != 12 {
		t.Fatalf("servers are not ordered by latency: %+v", list.Servers)
	}
}

func TestServerOptionsStopAtTheLimit(t *testing.T) {
	servers := make(speedtest.Servers, 0, serverListLimit+10)
	for i := range serverListLimit + 10 {
		servers = append(servers, &speedtest.Server{ID: strconv.Itoa(i), Sponsor: "Sponsor", Latency: time.Duration(i) * time.Millisecond})
	}
	list := serverOptions(servers)
	if len(list.Servers) != serverListLimit || list.Reachable != serverListLimit+10 {
		t.Fatalf("listed %d servers of %d", len(list.Servers), list.Reachable)
	}
}

func TestServerSearchIsTrimmedAndBounded(t *testing.T) {
	if search, err := serverSearch("  Taiwan  "); err != nil || search != "Taiwan" {
		t.Fatalf("search = %q, err = %v", search, err)
	}
	if _, err := serverSearch(strings.Repeat("x", 65)); err == nil {
		t.Fatal("over-long search was accepted")
	}
	if _, err := serverSearch("line\nbreak"); err == nil {
		t.Fatal("control characters were accepted")
	}
}

func TestServersReportWhatTheTargetAnswered(t *testing.T) {
	var gotExit, gotSearch string
	h := &HTTP{ListServers: func(_ context.Context, exit, search string) (ServerList, error) {
		gotExit, gotSearch = exit, search
		return ServerList{Servers: []ServerOption{{ID: "9", Sponsor: "Sponsor", Location: "City, Country", Latency: 20}}, Candidates: 12, Reachable: 4}, nil
	}}
	w := httptest.NewRecorder()
	core.RouteTable(h.Routes()).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/speedtest/servers?search=%20Taiwan%20", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if gotExit != "" || gotSearch != "Taiwan" {
		t.Fatalf("exit = %q, search = %q", gotExit, gotSearch)
	}
	var list ServerList
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if list.Candidates != 12 || list.Reachable != 4 || len(list.Servers) != 1 || list.Servers[0].ID != "9" {
		t.Fatalf("list = %+v", list)
	}
}

func TestServersRejectUnusableSearch(t *testing.T) {
	h := &HTTP{ListServers: func(context.Context, string, string) (ServerList, error) {
		t.Fatal("listed servers for an unusable search")
		return ServerList{}, nil
	}}
	routes := core.RouteTable(h.Routes())
	for _, query := range []string{"search=" + strings.Repeat("x", 80), "search=a%0Ab"} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			path := "/speedtest/servers"
			if method == http.MethodPost {
				path = "/speedtest"
			}
			w := httptest.NewRecorder()
			routes.ServeHTTP(w, httptest.NewRequest(method, path+"?"+query, nil))
			if w.Code != http.StatusBadRequest {
				t.Errorf("%s %s: got %d, want 400", method, path, w.Code)
			}
		}
	}
}

func TestServersReportListFailures(t *testing.T) {
	h := &HTTP{ListServers: func(context.Context, string, string) (ServerList, error) {
		return ServerList{}, errors.New("provider unreachable")
	}}
	w := httptest.NewRecorder()
	core.RouteTable(h.Routes()).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/speedtest/servers", nil))
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "provider unreachable") {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
}
