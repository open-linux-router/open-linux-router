package system

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublicAddressesEndpoint(t *testing.T) {
	ipv4 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ip" || r.Header.Get("User-Agent") != "open-linux-router" {
			t.Errorf("IPv4 request: %s %s", r.URL.Path, r.Header.Get("User-Agent"))
		}
		w.Write([]byte("203.0.113.42\n"))
	}))
	defer ipv4.Close()
	ipv6 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("2001:db8::42\n"))
	}))
	defer ipv6.Close()

	clientFor := func(target string) *http.Client {
		return &http.Client{Transport: rewriteTransport{target: target}}
	}
	h := HTTP{PublicIPv4Client: clientFor(ipv4.URL), PublicIPv6Client: clientFor(ipv6.URL)}
	rec := httptest.NewRecorder()
	h.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/public-addresses", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var got PublicAddresses
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.IPv4 != "203.0.113.42" || got.IPv6 != "2001:db8::42" {
		t.Fatalf("addresses: %+v", got)
	}
}

type rewriteTransport struct{ target string }

func (rt rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	url := rt.target + req.URL.Path
	rewritten, err := http.NewRequestWithContext(req.Context(), req.Method, url, nil)
	if err != nil {
		return nil, err
	}
	rewritten.Header = req.Header
	return http.DefaultTransport.RoundTrip(rewritten)
}

func TestReadPublicAddressRejectsWrongFamilyAndFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/wrong":
			w.Write([]byte("2001:db8::1\n"))
		case "/invalid":
			w.Write([]byte("not an address"))
		case "/error":
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		}
	}))
	defer server.Close()
	for _, path := range []string{"/wrong", "/invalid", "/error"} {
		if got := readPublicAddress(t.Context(), server.Client(), server.URL+path, true); got != "" {
			t.Errorf("%s: got %q", path, got)
		}
	}
	if got := readPublicAddress(t.Context(), server.Client(), server.URL+"/wrong", false); !strings.HasPrefix(got, "2001:db8:") {
		t.Errorf("IPv6: got %q", got)
	}
}

func TestPublicAddressesKeepSuccessfulFamily(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("198.51.100.7\n"))
	}))
	defer server.Close()
	h := HTTP{
		PublicIPv4Client: &http.Client{Transport: rewriteTransport{target: server.URL}},
		PublicIPv6Client: &http.Client{Transport: rewriteTransport{target: server.URL}},
	}
	rec := httptest.NewRecorder()
	h.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/public-addresses", nil))
	var got PublicAddresses
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.IPv4 != "198.51.100.7" || got.IPv6 != "" {
		t.Fatalf("addresses: %+v", got)
	}
}
