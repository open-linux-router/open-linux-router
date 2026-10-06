package system

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPublicAddressesEndpoint(t *testing.T) {
	ipv4 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/geoip" || r.Header.Get("User-Agent") != "open-linux-router" {
			t.Errorf("IPv4 request: %s %s", r.URL.Path, r.Header.Get("User-Agent"))
		}
		w.Write([]byte(`{"ip":"203.0.113.42","country":"Exampleland","city":"Example City","isp":"Example ISP","asn":64500}`))
	}))
	defer ipv4.Close()
	ipv6 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ip":"2001:db8::42","region":"Example Region"}`))
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
	if got.IPv4.IP != "203.0.113.42" || got.IPv4.ISP != "Example ISP" || got.IPv4.ASN != 64500 || got.IPv6.IP != "2001:db8::42" || got.IPv6.Region != "Example Region" {
		t.Fatalf("addresses: %+v", got)
	}
}

type rewriteTransport struct{ target string }

func (rt rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rewritten, err := http.NewRequestWithContext(req.Context(), req.Method, rt.target+req.URL.Path, nil)
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
			w.Write([]byte(`{"ip":"2001:db8::1"}`))
		case "/invalid":
			w.Write([]byte(`{"ip":"not an address"}`))
		case "/error":
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		}
	}))
	defer server.Close()
	for _, path := range []string{"/wrong", "/invalid", "/error"} {
		if got := readPublicAddress(t.Context(), server.Client(), server.URL+path, true); got.IP != "" {
			t.Errorf("%s: got %+v", path, got)
		}
	}
	if got := readPublicAddress(t.Context(), server.Client(), server.URL+"/wrong", false); got.IP != "2001:db8::1" {
		t.Errorf("IPv6: got %+v", got)
	}
}

func TestPublicAddressesKeepSuccessfulFamily(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ip":"198.51.100.7"}`))
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
	if got.IPv4.IP != "198.51.100.7" || got.IPv6.IP != "" {
		t.Fatalf("addresses: %+v", got)
	}
}
