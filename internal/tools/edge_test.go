package tools

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

func TestEdgeReturnsPartialReport(t *testing.T) {
	h := &HTTP{RunEdge: func(context.Context) (EdgeReport, error) {
		return EdgeReport{Edges: []EdgeResult{
			{CDN: "Cloudflare", POP: "HKG", Token: "HKG", Latency: 12.5},
			{CDN: "Fastly", Latency: 20, Error: "the answer carried no X-Served-By header"},
		}}, nil
	}}
	w := httptest.NewRecorder()
	core.RouteTable(h.Routes()).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/edge", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var report EdgeReport
	if err := json.Unmarshal(w.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Edges) != 2 {
		t.Fatalf("got %d edges", len(report.Edges))
	}
	if report.Edges[0].POP != "HKG" || report.Edges[1].Error == "" {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestEdgeRejectsWhenTheRunFails(t *testing.T) {
	h := &HTTP{RunEdge: func(context.Context) (EdgeReport, error) {
		return EdgeReport{}, errors.New("no route")
	}}
	w := httptest.NewRecorder()
	core.RouteTable(h.Routes()).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/edge", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
}

func TestReadCloudflareTrace(t *testing.T) {
	resp := &http.Response{Body: readCloser("fl=abc\nh=www.cloudflare.com\ncolo=NRT\nloc=JP\n")}
	token, err := readCloudflareTrace(resp)
	if err != nil || token != "NRT" {
		t.Fatalf("token %q err %v", token, err)
	}

	resp.Body = readCloser("fl=abc\nh=www.cloudflare.com\n")
	if _, err := readCloudflareTrace(resp); err == nil {
		t.Fatal("expected an error when the trace has no colo line")
	}
}

func TestReadHeader(t *testing.T) {
	resp := &http.Response{Header: http.Header{"X-Served-By": {"cache-sjc1000097-SJC, cache-hkg-vhhh8650052-HKG"}}}
	token, err := readHeader("X-Served-By", true)(resp)
	if err != nil || token != "cache-hkg-vhhh8650052-HKG" {
		t.Fatalf("token %q err %v", token, err)
	}

	resp.Header = http.Header{}
	if _, err := readHeader("X-Served-By", true)(resp); err == nil {
		t.Fatal("expected an error for a missing header")
	}
}

func TestEdgePOP(t *testing.T) {
	cases := []struct{ cdn, token, want string }{
		{"Cloudflare", "HKG", "HKG"},
		{"CloudFront", "HKG61-P1", "HKG"},
		{"CloudFront", "SJC", "SJC"},
		{"Fastly", "cache-hkg-vhhh8650052-HKG", "HKG"},
		{"Fastly", "cache-sjc1000097-SJC", "SJC"},
	}
	for _, c := range cases {
		if got := edgePOP(c.cdn, c.token); got != c.want {
			t.Errorf("%s %s: got %q want %q", c.cdn, c.token, got, c.want)
		}
	}
}

func TestMeasureEdgeAsksEveryProvider(t *testing.T) {
	cloudflare := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("fl=abc\ncolo=HND\nloc=JP\n"))
	}))
	defer cloudflare.Close()
	cloudfront := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Amz-Cf-Pop", "KIX50-P2")
	}))
	defer cloudfront.Close()
	fastly := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Served-By", "cache-hnd1-HND")
	}))
	defer fastly.Close()

	original := edgeProbes
	edgeProbes = []edgeProbe{
		{cdn: "Cloudflare", url: cloudflare.URL, read: readCloudflareTrace},
		{cdn: "CloudFront", url: cloudfront.URL, read: readHeader("X-Amz-Cf-Pop", false)},
		{cdn: "Fastly", url: fastly.URL, read: readHeader("X-Served-By", true)},
	}
	defer func() { edgeProbes = original }()

	report, err := measureEdge(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"HND", "KIX", "HND"}
	for i, edge := range report.Edges {
		if edge.Error != "" {
			t.Fatalf("%s: %s", edge.CDN, edge.Error)
		}
		if edge.POP != want[i] {
			t.Fatalf("%s: got POP %q want %q", edge.CDN, edge.POP, want[i])
		}
	}
}

func readCloser(s string) io.ReadCloser { return io.NopCloser(strings.NewReader(s)) }
