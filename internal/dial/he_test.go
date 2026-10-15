package dial

import (
	"encoding/json"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

func TestHECatalog(t *testing.T) {
	w := httptest.NewRecorder()
	HTTP{}.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/he/servers", nil))
	var servers []heServer
	if err := json.Unmarshal(w.Body.Bytes(), &servers); err != nil {
		t.Fatal(err)
	}
	if len(servers) != 36 {
		t.Fatalf("got %d servers", len(servers))
	}
	seen := map[string]bool{}
	for _, s := range servers {
		if s.Name == "Warsaw, PL" {
			if s.Address != "" {
				t.Fatal("full Warsaw server has an invented endpoint")
			}
			continue
		}
		a, err := netip.ParseAddr(s.Address)
		if err != nil || !a.Is4() || seen[s.Address] {
			t.Fatalf("bad endpoint: %+v", s)
		}
		seen[s.Address] = true
	}
}

func TestHECheckRejectsUnconfiguredAndArbitraryTargets(t *testing.T) {
	for _, tc := range []struct {
		document, body string
		status         int
	}{
		{``, `{}`, 422},
		{`{"dial":{"uplink":{"interface":"wan0"}}}`, `{"target":"127.0.0.1"}`, 400},
		{`{"dial":{"uplink":{"interface":"wan0"}}}`, `{"target":"2001:4860:4860::8888"}`, 400},
		{`{"dial":{"uplink":{"interface":"wan0"}}}`, `{"target":"example.com"}`, 400},
		{``, `{"unexpected":true}`, 400},
	} {
		h := HTTP{Applier: Applier{Store: testStore(t, tc.document)}}.Handler()
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", "/he/check", strings.NewReader(tc.body)))
		if w.Code != tc.status {
			t.Fatalf("%s: got %d: %s", tc.body, w.Code, w.Body.String())
		}
	}
}
