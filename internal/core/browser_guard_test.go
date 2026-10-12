package core

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBrowserGuardRejectsCrossSiteFormsAndRebinding(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	guard := BrowserGuard(next, []string{"router.example"})
	for _, tc := range []struct {
		host, origin, media, site string
		tls                       bool
		code                      int
	}{
		{"192.168.1.1:8080", "http://192.168.1.1:8080", "application/json", "same-origin", false, 200},
		{"router.example", "https://router.example", "application/json", "same-origin", true, 200},
		{"attacker.example", "http://attacker.example", "application/json", "same-origin", false, 403},
		{"192.168.1.1:8080", "http://attacker.example", "text/plain", "", false, 403},
		{"192.168.1.1:8080", "", "text/plain", "", false, 415},
		{"192.168.1.1:8080", "null", "application/json", "", false, 403},
		{"192.168.1.1:8080", "", "application/json", "cross-site", false, 403},
		{"192.168.1.1:8080", "", "application/json", "", false, 200},
		{"192.168.1.1:8080", "https://192.168.1.1:8080", "application/json", "", false, 403},
	} {
		r := httptest.NewRequest("POST", "http://"+tc.host+"/api/system/access/claim", strings.NewReader(`{"no_password":true}`))
		r = r.WithContext(context.WithValue(r.Context(), http.LocalAddrContextKey, &net.TCPAddr{IP: net.ParseIP("192.168.1.1"), Port: 8080}))
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
