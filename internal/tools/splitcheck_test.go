package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/geoip"
)

func TestSplitCheckGroupsAnswers(t *testing.T) {
	original := splitProbes
	splitProbes = []splitProbe{
		{name: "one", region: "Global", url: textServer(t, "203.0.113.9\n"), read: readBareAddress},
		{name: "two", region: "Global", url: textServer(t, "203.0.113.9"), read: readBareAddress},
		{name: "three", region: "China", url: textServer(t, "198.51.100.4"), read: readBareAddress},
	}
	defer func() { splitProbes = original }()

	h := &HTTP{Locate: func(context.Context, []netip.Addr) (map[string]geoip.Place, error) {
		return map[string]geoip.Place{
			"203.0.113.9":  {City: "Hong Kong", Country: "HK", CountryName: "Hong Kong"},
			"198.51.100.4": {City: "Beijing", Country: "CN", CountryName: "China"},
		}, nil
	}}
	w := httptest.NewRecorder()
	core.RouteTable(h.Routes()).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/split-check", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var report SplitReport
	if err := json.Unmarshal(w.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Probes) != 3 {
		t.Fatalf("got %d probes", len(report.Probes))
	}
	if len(report.Egresses) != 2 {
		t.Fatalf("got %d egresses: %+v", len(report.Egresses), report.Egresses)
	}
	if got := report.Egresses[0].Probes; len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Fatalf("grouping: %+v", report.Egresses)
	}
	if report.Egresses[0].Place == nil || report.Egresses[0].Place.City != "Hong Kong" {
		t.Fatalf("place: %+v", report.Egresses[0])
	}
	if report.Probes[2].Place == nil || report.Probes[2].Place.City != "Beijing" {
		t.Fatalf("probe place: %+v", report.Probes[2])
	}
}

func TestSplitCheckKeepsAFailedProbe(t *testing.T) {
	original := splitProbes
	splitProbes = []splitProbe{
		{name: "one", region: "Global", url: textServer(t, "203.0.113.9"), read: readBareAddress},
		{name: "refuses", region: "Global", url: refusingServer(t), read: readBareAddress},
	}
	defer func() { splitProbes = original }()

	report, err := measureSplit(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if report.Probes[1].Error == "" {
		t.Fatalf("a failed probe carried no error: %+v", report.Probes[1])
	}
	if len(report.Egresses) != 1 {
		t.Fatalf("a failed probe was grouped: %+v", report.Egresses)
	}
}

func TestSplitReaders(t *testing.T) {
	trace := &http.Response{
		Body: readCloser("fl=abc\nip=203.0.113.7\nts=1\n"),
	}
	if ip, err := readTraceIP(trace); err != nil || ip != "203.0.113.7" {
		t.Fatalf("trace: %q %v", ip, err)
	}
	sentence := &http.Response{
		Body: readCloser("当前 IP：198.51.100.8  来自于：中国 北京 北京  联通"),
	}
	if ip, err := readAddressInText(sentence); err != nil || ip != "198.51.100.8" {
		t.Fatalf("sentence: %q %v", ip, err)
	}
	nothing := &http.Response{Body: readCloser("no address here")}
	if _, err := readAddressInText(nothing); err == nil {
		t.Fatal("accepted an answer with no address")
	}
}

func textServer(t *testing.T, body string) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func refusingServer(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no", http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)
	return server.URL
}
