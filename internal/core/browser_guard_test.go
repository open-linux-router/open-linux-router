package core

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBrowserGuardSupportsCustomDomainsAndReverseProxies(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	guard := BrowserGuard(next)
	for _, tc := range []struct {
		host, origin, media, site string
		tls                       bool
		code                      int
	}{
		{"192.168.1.1:8080", "http://192.168.1.1:8080", "application/json", "same-origin", false, 200},
		{"router.example", "https://router.example", "application/json", "same-origin", true, 200},
		{"olr.home.iloahz.com", "https://olr.home.iloahz.com", "application/json", "same-origin", false, 200},
		{"192.168.1.1:8080", "http://other.example", "text/plain", "", false, 415},
		{"192.168.1.1:8080", "", "text/plain", "", false, 415},
		{"192.168.1.1:8080", "null", "application/json", "", false, 200},
		{"192.168.1.1:8080", "", "application/json", "cross-site", false, 403},
		{"192.168.1.1:8080", "", "application/json", "", false, 200},
		{"192.168.1.1:8080", "https://192.168.1.1:8080", "application/json", "", false, 200},
	} {
		r := httptest.NewRequest("POST", "http://"+tc.host+"/api/system/access/claim", strings.NewReader(`{"no_password":true}`))
		if tc.tls {
			r.TLS = &tls.ConnectionState{}
		}
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("Content-Type", tc.media)
		r.Header.Set("Sec-Fetch-Site", tc.site)
		w := httptest.NewRecorder()
		guard.ServeHTTP(w, r)
		if w.Code != tc.code {
			t.Fatalf("%+v: %d %s", tc, w.Code, w.Body)
		}
	}
}

func TestBrowserGuardLoadsSPAThroughCustomDomain(t *testing.T) {
	guard := BrowserGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("spa"))
	}))
	for _, path := range []string{"/", "/access/ingress", "/assets/app.js"} {
		r := httptest.NewRequest("GET", "http://olr.home.iloahz.com"+path, nil)
		w := httptest.NewRecorder()
		guard.ServeHTTP(w, r)
		if w.Code != http.StatusOK || w.Body.String() != "spa" {
			t.Fatalf("GET %s: %d %s", path, w.Code, w.Body)
		}
	}
}
