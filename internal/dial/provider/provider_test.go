package provider

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// Every provider here is tested against an httptest server that asserts the
// request it was given, the way internal/dnsrelay tests byte-for-byte against a
// fake upstream. That is the only kind of test worth having for this code: the
// logic is a handful of branches and the value is in the exact bytes on the
// wire, because a signature that is one character out authenticates against
// nothing and a field named `SubDomain` where the API wants `Subdomain` fails
// only at the vendor.

// rewrite points a client at the test server while leaving the path, query,
// headers and body exactly as the provider built them.
//
// Preferred over making each endpoint a settable field: the endpoints are not
// configuration, and a test that can change them is a test that no longer
// checks the provider talks to the right place.
type rewrite struct {
	base  *url.URL
	inner http.RoundTripper
}

func (rw rewrite) RoundTrip(req *http.Request) (*http.Response, error) {
	out := req.Clone(req.Context())
	out.URL.Scheme = rw.base.Scheme
	out.URL.Host = rw.base.Host
	out.Host = ""
	return rw.inner.RoundTrip(out)
}

// clientFor returns a client that sends everything to srv.
func clientFor(t *testing.T, srv *httptest.Server) *http.Client {
	t.Helper()
	base, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Transport: rewrite{base: base, inner: http.DefaultTransport}}
}

// call is one request the fake vendor saw.
type call struct {
	Method string
	Path   string
	Query  url.Values
	Header http.Header
	Body   string
}

// recorder collects the calls so a test can assert on what was sent as well as
// on what came back.
type recorder struct {
	t     *testing.T
	calls []call
}

func (r *recorder) record(req *http.Request) call {
	r.t.Helper()
	body, err := io.ReadAll(req.Body)
	if err != nil {
		r.t.Fatal(err)
	}
	c := call{
		Method: req.Method,
		Path:   req.URL.Path,
		Query:  req.URL.Query(),
		Header: req.Header.Clone(),
		Body:   string(body),
	}
	r.calls = append(r.calls, c)
	return c
}

func (r *recorder) count(method, pathContains string) int {
	n := 0
	for _, c := range r.calls {
		if c.Method == method && strings.Contains(c.Path, pathContains) {
			n++
		}
	}
	return n
}

func TestForRefusesAnUnknownName(t *testing.T) {
	// docs/ddns.md §4.4: upstream's dispatch ends in `default: &Alidns{}`, so a
	// typo publishes to Alibaba DNS. There is no default here.
	if _, err := For("clouflare", nil); err == nil {
		t.Fatal("a misspelled provider name should be refused, not defaulted")
	}
	for _, name := range Names() {
		if _, err := For(name, nil); err != nil {
			t.Errorf("For(%q) = %v; every name in Names() must be implemented", name, err)
		}
	}
}

func TestSubdomainSplitsOnTheZone(t *testing.T) {
	for _, tc := range []struct{ name, zone, want string }{
		{"home.example.net", "example.net", "home"},
		{"a.b.example.net", "example.net", "a.b"},
		{"example.net", "example.net", "@"},
		{"Home.Example.Net", "example.net", "home"},
		{"home.example.net.", "example.net", "home"},
	} {
		if got := (Record{Name: tc.name, Zone: tc.zone}).Subdomain(); got != tc.want {
			t.Errorf("Subdomain(%q in %q) = %q, want %q", tc.name, tc.zone, got, tc.want)
		}
	}
}
