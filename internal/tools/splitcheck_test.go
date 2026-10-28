package tools

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/geoip"
)

func TestMeasureSplitGroupsByEgress(t *testing.T) {
	fleet := []splitServer{
		{host: "a.example:8080", country: "Hong Kong", sponsor: "A"},
		{host: "b.example:8080", country: "Japan", sponsor: "B"},
		{host: "c.example:8080", country: "China", sponsor: "C"},
	}
	probe := func(_ context.Context, server splitServer) (string, error) {
		if strings.HasPrefix(server.host, "c.") {
			return "198.51.100.4", nil
		}
		return "203.0.113.9", nil
	}
	locate := func(context.Context, []netip.Addr) (map[string]geoip.Place, error) {
		return map[string]geoip.Place{
			"203.0.113.9":  {City: "Hong Kong", Country: "HK", CountryName: "Hong Kong"},
			"198.51.100.4": {City: "Beijing", Country: "CN", CountryName: "China"},
		}, nil
	}
	report, err := measureSplit(context.Background(),
		func(context.Context) ([]splitServer, error) { return fleet, nil }, probe, locate)
	if err != nil {
		t.Fatal(err)
	}
	if report.Asked != 3 || report.Answered != 3 {
		t.Fatalf("totals: %+v", report)
	}
	if len(report.Egresses) != 2 {
		t.Fatalf("got %d egresses: %+v", len(report.Egresses), report.Egresses)
	}
	// The larger group first.
	if report.Egresses[0].IP != "203.0.113.9" || report.Egresses[0].Count != 2 {
		t.Fatalf("first egress: %+v", report.Egresses[0])
	}
	if got := report.Egresses[0].Countries; len(got) != 2 || got[0] != "Hong Kong" || got[1] != "Japan" {
		t.Fatalf("countries: %+v", got)
	}
	if report.Egresses[0].Place == nil || report.Egresses[0].Place.City != "Hong Kong" {
		t.Fatalf("place: %+v", report.Egresses[0])
	}
	if report.Egresses[1].IP != "198.51.100.4" || report.Egresses[1].Count != 1 {
		t.Fatalf("second egress: %+v", report.Egresses[1])
	}
}

func TestMeasureSplitKeepsFailuresInTheDenominator(t *testing.T) {
	fleet := []splitServer{
		{host: "a.example:8080", country: "Hong Kong", sponsor: "A"},
		{host: "b.example:8080", country: "Japan", sponsor: "B"},
	}
	probe := func(_ context.Context, server splitServer) (string, error) {
		if strings.HasPrefix(server.host, "b.") {
			return "", errors.New("HTTP 503")
		}
		return "203.0.113.9", nil
	}
	report, err := measureSplit(context.Background(),
		func(context.Context) ([]splitServer, error) { return fleet, nil }, probe, nil)
	if err != nil {
		t.Fatal(err)
	}
	if report.Asked != 2 || report.Answered != 1 {
		t.Fatalf("totals: %+v", report)
	}
	if len(report.Failures) != 1 || !strings.Contains(report.Failures[0].Error, "503") {
		t.Fatalf("failures: %+v", report.Failures)
	}
	if len(report.Egresses) != 1 || report.Egresses[0].Count != 1 {
		t.Fatalf("egresses: %+v", report.Egresses)
	}
}

func TestMeasureSplitReportsAnEmptyFleet(t *testing.T) {
	_, err := measureSplit(context.Background(),
		func(context.Context) ([]splitServer, error) { return nil, errors.New("list unavailable") }, nil, nil)
	if err == nil {
		t.Fatal("an unavailable fleet was accepted")
	}
}

func TestSearchSplitCountryReadsTheFleet(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("search") != "Japan" || r.URL.Query().Get("engine") != "js" {
			t.Errorf("query: %s", r.URL.RawQuery)
		}
		if r.Header.Get("Referer") == "" {
			t.Error("no referer")
		}
		_, _ = w.Write([]byte(`[{"host":"a.example:8080","country":"Japan","sponsor":"A"},{"host":"","country":"X"}]`))
	}))
	defer server.Close()
	original := splitServersURL
	splitServersURL = server.URL
	defer func() { splitServersURL = original }()

	servers, err := searchSplitCountry(context.Background(), "Japan")
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 1 || servers[0].host != "a.example:8080" || servers[0].country != "Japan" {
		t.Fatalf("servers: %+v", servers)
	}
}

func TestProbeSplitServerFollowsTheRedirect(t *testing.T) {
	canonical := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/getip" {
			t.Errorf("path: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte("203.0.113.9\n"))
	}))
	defer canonical.Close()
	alias := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, canonical.URL+"/getip", http.StatusTemporaryRedirect)
	}))
	defer alias.Close()

	ip, err := probeSplitServer(context.Background(), splitServer{host: strings.TrimPrefix(alias.URL, "http://")})
	if err != nil {
		t.Fatal(err)
	}
	if ip != "203.0.113.9" {
		t.Fatalf("ip: %q", ip)
	}
}

func TestProbeSplitServerRejectsAnAnswerThatIsNotAnAddress(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html>no</html>"))
	}))
	defer server.Close()
	if _, err := probeSplitServer(context.Background(), splitServer{host: strings.TrimPrefix(server.URL, "http://")}); err == nil {
		t.Fatal("accepted a non-address answer")
	}
}

func TestHostWithPort(t *testing.T) {
	if got := hostWithPort("a.example"); got != "a.example:8080" {
		t.Fatalf("got %q", got)
	}
	if got := hostWithPort("a.example:8080"); got != "a.example:8080" {
		t.Fatalf("got %q", got)
	}
}

func TestSplitCheckServesTheReport(t *testing.T) {
	originalFleet, originalProbe := splitServers, splitProbeOnce
	splitServers = func(context.Context) ([]splitServer, error) {
		return []splitServer{{host: "a.example:8080", country: "Japan", sponsor: "A"}}, nil
	}
	splitProbeOnce = func(context.Context, splitServer) (string, error) { return "203.0.113.9", nil }
	defer func() { splitServers, splitProbeOnce = originalFleet, originalProbe }()

	h := &HTTP{}
	w := httptest.NewRecorder()
	core.RouteTable(h.Routes()).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/split-check", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var report SplitReport
	if err := json.Unmarshal(w.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Asked != 1 || len(report.Egresses) != 1 || report.Egresses[0].IP != "203.0.113.9" {
		t.Fatalf("report: %+v", report)
	}
}

func TestProbeSplitServerRejectsAPrivateAddress(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("192.168.1.20"))
	}))
	defer server.Close()
	if _, err := probeSplitServer(context.Background(), splitServer{host: strings.TrimPrefix(server.URL, "http://")}); err == nil {
		t.Fatal("accepted a private address as an egress")
	}
}
