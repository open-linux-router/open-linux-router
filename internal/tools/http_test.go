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

// TestSpeedtestProgressFollowsTheStages pins what a page may show while the
// POST is still open: the stage actually running, the values it has settled,
// and a live rate only for the direction being measured.
func TestSpeedtestProgressFollowsTheStages(t *testing.T) {
	h := &HTTP{}
	h.startSpeed(speedOokla, "wan2", speedPhaseServer)
	h.setSpeedServer("hong", "Hong Kong")
	h.setSpeedPhase(speedPhaseLatency)
	h.setSpeedValue(speedPhaseLatency, 8.2)
	h.setSpeedPhase(speedPhaseDownload)
	// The library's callback for a direction that is not the one running must
	// not publish a rate: it would label a finished stage as live.
	h.setSpeedLive(speedPhaseUpload, 999)
	h.setSpeedLive(speedPhaseDownload, 321.5)

	w := httptest.NewRecorder()
	core.RouteTable(h.Routes()).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/speedtest/progress", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var progress SpeedProgress
	if err := json.Unmarshal(w.Body.Bytes(), &progress); err != nil {
		t.Fatal(err)
	}
	if !progress.Running || progress.Phase != speedPhaseDownload {
		t.Fatalf("progress is not in the download stage: %+v", progress)
	}
	if progress.Provider != speedOokla || progress.Exit != "wan2" || progress.Server != "hong" || progress.Location != "Hong Kong" {
		t.Fatalf("progress lost the run's identity: %+v", progress)
	}
	if progress.Latency == nil || *progress.Latency != 8.2 {
		t.Fatalf("latency = %v, want the value the ping stage settled", progress.Latency)
	}
	if progress.Download != nil || progress.Upload != nil {
		t.Fatalf("unfinished stages were reported as measured: %+v", progress)
	}
	if progress.Live == nil || *progress.Live != 321.5 {
		t.Fatalf("live = %v, want the download rate only", progress.Live)
	}
	if progress.ElapsedMS < 0 {
		t.Fatalf("elapsed_ms = %d", progress.ElapsedMS)
	}
}

func TestSpeedtestProgressSettlesWhenTheRunEnds(t *testing.T) {
	h := &HTTP{
		ResolveExit: func(name string) (uint32, error) {
			if name != "wan2" {
				t.Fatalf("resolved %q", name)
			}
			return 0x00030000, nil
		},
		Run: func(context.Context) (SpeedResult, error) {
			return SpeedResult{Server: "Test", Location: "Somewhere", Latency: 7.5, Download: 123.4, Upload: 56.7}, nil
		},
	}
	routes := core.RouteTable(h.Routes())
	routes.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/speedtest?exit=wan2", nil))

	w := httptest.NewRecorder()
	routes.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/speedtest/progress", nil))
	var progress SpeedProgress
	if err := json.Unmarshal(w.Body.Bytes(), &progress); err != nil {
		t.Fatal(err)
	}
	if progress.Running || progress.Phase != speedPhaseDone || progress.Live != nil {
		t.Fatalf("finished run still reports progress: %+v", progress)
	}
	if progress.Download == nil || *progress.Download != 123.4 || progress.Upload == nil || *progress.Upload != 56.7 {
		t.Fatalf("finished run lost its values: %+v", progress)
	}
	if progress.Server != "Test" || progress.Location != "Somewhere" || progress.Exit != "wan2" {
		t.Fatalf("finished run lost its server: %+v", progress)
	}
}

func TestSpeedtestProgressRecordsFailureAndStops(t *testing.T) {
	h := &HTTP{Run: func(context.Context) (SpeedResult, error) {
		return SpeedResult{}, errors.New("no usable test server among the first three candidates")
	}}
	routes := core.RouteTable(h.Routes())
	w := httptest.NewRecorder()
	routes.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/speedtest", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("failing test returned %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	routes.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/speedtest/progress", nil))
	var progress SpeedProgress
	if err := json.Unmarshal(w.Body.Bytes(), &progress); err != nil {
		t.Fatal(err)
	}
	if progress.Running {
		t.Fatalf("a failed run is still reported as running: %+v", progress)
	}
	if !strings.Contains(progress.Error, "no usable test server") {
		t.Fatalf("error = %q, want the run's failure", progress.Error)
	}
	if progress.Phase != speedPhaseServer {
		t.Fatalf("phase = %q; a failed run keeps the stage it failed in", progress.Phase)
	}
}

func TestSpeedtestProgressIsReadableWhileTheTestRuns(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	h := &HTTP{Run: func(context.Context) (SpeedResult, error) {
		close(started)
		<-release
		return SpeedResult{Server: "Test", Download: 10, Upload: 2}, nil
	}}
	routes := core.RouteTable(h.Routes())
	done := make(chan struct{})
	go func() {
		defer close(done)
		routes.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/speedtest", nil))
	}()
	<-started

	w := httptest.NewRecorder()
	routes.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/speedtest/progress", nil))
	var progress SpeedProgress
	if err := json.Unmarshal(w.Body.Bytes(), &progress); err != nil {
		t.Fatal(err)
	}
	if !progress.Running || progress.Phase != speedPhaseServer {
		t.Fatalf("progress while the POST is open = %+v", progress)
	}

	close(release)
	<-done
	w = httptest.NewRecorder()
	routes.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/speedtest/progress", nil))
	if err := json.Unmarshal(w.Body.Bytes(), &progress); err != nil {
		t.Fatal(err)
	}
	if progress.Running {
		t.Fatalf("progress still running after the POST answered: %+v", progress)
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

func TestServerOptionsKeepProviderOrderAndCount(t *testing.T) {
	rows := []providerServer{
		{ID: "1", Sponsor: "Near", Name: "City", Country: "Country A"},
		{ID: "", Sponsor: "Unidentified", Name: "Town", Country: "Country B"},
		{ID: "3", Sponsor: "Far", Name: "Town", Country: "Country C"},
	}
	list := serverOptions(rows)
	if list.Total != 2 || len(list.Servers) != 2 {
		t.Fatalf("list = %+v", list)
	}
	if list.Servers[0].ID != "1" || list.Servers[1].Location != "Town, Country C" {
		t.Fatalf("servers did not keep the provider order: %+v", list.Servers)
	}
}

func TestServerOptionsStopAtTheLimit(t *testing.T) {
	rows := make([]providerServer, 0, serverListLimit+10)
	for i := range serverListLimit + 10 {
		rows = append(rows, providerServer{ID: strconv.Itoa(i), Sponsor: "Sponsor", Name: "City"})
	}
	list := serverOptions(rows)
	if len(list.Servers) != serverListLimit || list.Total != serverListLimit+10 {
		t.Fatalf("listed %d servers of %d", len(list.Servers), list.Total)
	}
}

func TestServerLabelsSkipMissingParts(t *testing.T) {
	for _, test := range []struct{ name, country, want string }{
		{"Keelung", "Taiwan", "Keelung, Taiwan"},
		{"Hong Kong", "", "Hong Kong"},
		{"", "Taiwan", "Taiwan"},
		{"Keelung", "Keelung", "Keelung"},
	} {
		if got := place(test.name, test.country); got != test.want {
			t.Errorf("place(%q, %q) = %q, want %q", test.name, test.country, got, test.want)
		}
	}
}

func TestProviderServersAskForTheSearchTerm(t *testing.T) {
	var gotQuery string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("search")
		_, _ = w.Write([]byte(`[{"id":"9","sponsor":"Sponsor","name":"Keelung","country":"Taiwan"}]`))
	}))
	defer provider.Close()
	rows, err := providerServers(context.Background(), provider.Client(), provider.URL, "Taiwan")
	if err != nil {
		t.Fatal(err)
	}
	if gotQuery != "Taiwan" || len(rows) != 1 || rows[0].ID != "9" {
		t.Fatalf("search = %q, rows = %+v", gotQuery, rows)
	}
}

func TestProviderServersReportBadResponses(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer provider.Close()
	if _, err := providerServers(context.Background(), provider.Client(), provider.URL, ""); err == nil {
		t.Fatal("a refused request was treated as a server list")
	}
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"servers":[]}`))
	}))
	defer broken.Close()
	if _, err := providerServers(context.Background(), broken.Client(), broken.URL, ""); err == nil {
		t.Fatal("a payload that is not a server list was accepted")
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

func TestServersReturnTheProviderList(t *testing.T) {
	var gotExit, gotSearch string
	h := &HTTP{ListServers: func(_ context.Context, exit, search string) (ServerList, error) {
		gotExit, gotSearch = exit, search
		return ServerList{Servers: []ServerOption{{ID: "9", Sponsor: "Sponsor", Location: "City, Country"}}, Total: 12}, nil
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
	if list.Total != 12 || len(list.Servers) != 1 || list.Servers[0].ID != "9" {
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

func TestSpeedtestRunsTheSelectedServiceThroughTheWayOut(t *testing.T) {
	var gotMark uint32
	h := &HTTP{
		ResolveExit: func(name string) (uint32, error) {
			if name != "wan2" {
				t.Fatalf("resolved %q", name)
			}
			return 0x00030000, nil
		},
		RunCloudflare: func(_ context.Context, mark uint32) (SpeedResult, error) {
			gotMark = mark
			return SpeedResult{Server: "Cloudflare", Download: 100, Upload: 10}, nil
		},
	}
	w := httptest.NewRecorder()
	core.RouteTable(h.Routes()).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/speedtest?provider=cloudflare&exit=wan2", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if gotMark != 0x00030000 {
		t.Fatalf("service ran with mark %#x", gotMark)
	}
	var result SpeedResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Server != "Cloudflare" || result.Exit != "wan2" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

// A run that outlives the handler's deadline is the one failure the operator
// can act on: the page has to say the test gave up rather than hand them the
// library's "context deadline exceeded".
func TestSpeedtestNamesTheDeadline(t *testing.T) {
	h := &HTTP{RunCloudflare: func(context.Context, uint32) (SpeedResult, error) {
		return SpeedResult{}, context.DeadlineExceeded
	}}
	w := httptest.NewRecorder()
	core.RouteTable(h.Routes()).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/speedtest?provider=cloudflare", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if body := w.Body.String(); !strings.Contains(body, "90 seconds") || strings.Contains(body, "context deadline") {
		t.Fatalf("deadline reported as %s", body)
	}
}

func TestSpeedtestRejectsUnknownServiceAndWayOut(t *testing.T) {
	h := &HTTP{
		ResolveExit: func(string) (uint32, error) { return 0, errors.New("way out %q is unavailable") },
		RunFast:     func(context.Context, uint32) (SpeedResult, error) { return SpeedResult{}, nil },
	}
	routes := core.RouteTable(h.Routes())
	for _, target := range []string{"/speedtest?provider=nonsense", "/speedtest?provider=fastcom&exit=gone"} {
		w := httptest.NewRecorder()
		routes.ServeHTTP(w, httptest.NewRequest(http.MethodPost, target, nil))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s returned %d, want 400: %s", target, w.Code, w.Body.String())
		}
	}
}

func TestProvidersReportWhichServicesAreInstalled(t *testing.T) {
	h := &HTTP{LookTool: func(name string) string {
		if name == "fast-cli" {
			return "/usr/bin/fast-cli"
		}
		return ""
	}}
	w := httptest.NewRecorder()
	core.RouteTable(h.Routes()).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/speedtest/providers", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Providers []SpeedProvider `json:"providers"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	got := map[string]SpeedProvider{}
	for _, provider := range body.Providers {
		got[provider.ID] = provider
	}
	if len(got) != 3 || !got["ookla"].Available {
		t.Fatalf("providers = %+v", body.Providers)
	}
	if got["cloudflare"].Available || got["cloudflare"].Detail == "" {
		t.Fatalf("cloudflare should be unavailable with a reason: %+v", got["cloudflare"])
	}
	if !got["fastcom"].Available {
		t.Fatalf("fast.com should be available: %+v", got["fastcom"])
	}
}
