package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func callbackRecordFixture() Record {
	return Record{
		Name:  "home.example.net",
		Zone:  "example.net",
		Type:  "A",
		Value: "203.0.113.9",
		TTL:   600,
	}
}

// The DynDNS v2 shape this provider exists for: one GET with the address in the
// query string. No-IP, DuckDNS and Dynu are all this request with a different
// host.
func TestCallbackGetsWithTheAddressSubstituted(t *testing.T) {
	rec := &recorder{t: t}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(r)
		_, _ = w.Write([]byte("good 203.0.113.9"))
	}))
	defer srv.Close()

	r := callbackRecordFixture()
	r.URL = srv.URL + "/nic/update?hostname=#{domain}&myip=#{ip}&token=t0ken"

	cb := callback{client: clientFor(t, srv)}
	if err := cb.Update(context.Background(), r); err != nil {
		t.Fatalf("Update: %v", err)
	}

	call := rec.calls[0]
	if call.Method != http.MethodGet {
		t.Errorf("method = %s, want GET when there is no body", call.Method)
	}
	if got := call.Query.Get("myip"); got != "203.0.113.9" {
		t.Errorf("myip = %q", got)
	}
	if got := call.Query.Get("hostname"); got != "home.example.net" {
		t.Errorf("hostname = %q", got)
	}
}

// A body turns the request into a POST, and its content type is sniffed rather
// than declared — the same field carries a form body for one endpoint and JSON
// for the next.
func TestCallbackPostsAJSONBodyAsJSON(t *testing.T) {
	rec := &recorder{t: t}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(r)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	r := callbackRecordFixture()
	r.URL = srv.URL + "/hook"
	r.Token = `{"name":"#{domain}","address":"#{ip}","type":"#{recordType}","ttl":#{ttl}}`

	cb := callback{client: clientFor(t, srv)}
	if err := cb.Update(context.Background(), r); err != nil {
		t.Fatalf("Update: %v", err)
	}

	call := rec.calls[0]
	if call.Method != http.MethodPost {
		t.Errorf("method = %s, want POST when there is a body", call.Method)
	}
	if got := call.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json for a body that is valid JSON", got)
	}
	want := `{"name":"home.example.net","address":"203.0.113.9","type":"A","ttl":600}`
	if call.Body != want {
		t.Errorf("body = %s\nwant     %s", call.Body, want)
	}
}

func TestCallbackPostsANonJSONBodyAsForm(t *testing.T) {
	rec := &recorder{t: t}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(r)
	}))
	defer srv.Close()

	r := callbackRecordFixture()
	r.URL = srv.URL + "/hook"
	r.Token = "ip=#{ip}&host=#{domain}"

	cb := callback{client: clientFor(t, srv)}
	if err := cb.Update(context.Background(), r); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got := rec.calls[0].Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
		t.Errorf("Content-Type = %q", got)
	}
}

// There is no record to read back, so the status is the whole of what this
// protocol says — and a refusal has to carry the endpoint's own words, because
// `badauth` and `nohost` are the two answers that mean different things.
func TestCallbackReportsTheEndpointsWords(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("badauth"))
	}))
	defer srv.Close()

	r := callbackRecordFixture()
	r.URL = srv.URL + "/nic/update?myip=#{ip}"

	cb := callback{client: clientFor(t, srv)}
	err := cb.Update(context.Background(), r)
	if err == nil {
		t.Fatal("a 401 is a failure")
	}
	if !strings.Contains(err.Error(), "badauth") {
		t.Errorf("err = %v; want the endpoint's own answer", err)
	}
	if !strings.Contains(err.Error(), "home.example.net") {
		t.Errorf("err = %v; want the record named", err)
	}
}

// `#{ipv6Addr}` is deliberately not substituted: v1 publishes an A record, and
// an unexpanded placeholder is the version of that failure an operator can see.
func TestCallbackLeavesTheIPv6PlaceholderAlone(t *testing.T) {
	cb := callback{}
	got := cb.expand("a=#{ip} b=#{ipv6Addr}", callbackRecordFixture())
	if !strings.Contains(got, "#{ipv6Addr}") {
		t.Errorf("expand(...) = %q; the IPv6 placeholder should survive rather than empty out", got)
	}
	if !strings.Contains(got, "203.0.113.9") {
		t.Errorf("expand(...) = %q; the address should be substituted", got)
	}
}
