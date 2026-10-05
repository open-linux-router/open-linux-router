package inspection

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStartRequiresExplicitIPv4(t *testing.T) {
	cases := []struct {
		name, ip string
		want     int
	}{
		{"missing", "", http.StatusBadRequest},
		{"invalid", "172.16.1.999", http.StatusBadRequest},
		{"IPv6", "fd00::1", http.StatusBadRequest},
		{"unspecified", "0.0.0.0", http.StatusBadRequest},
		{"multicast", "224.0.0.1", http.StatusBadRequest},
		{"explicit address", "172.16.1.135", http.StatusServiceUnavailable},
		{"no private restriction", "8.8.8.8", http.StatusServiceUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &Service{Enabled: true}
			req := httptest.NewRequest(http.MethodPost, "/session", strings.NewReader(fmt.Sprintf(`{"mac":"c4:c1:7d:e0:a3:65","ip":%q}`, tc.ip)))
			rec := httptest.NewRecorder()
			// No proxy binary on PATH: valid addresses pass targeting validation.
			t.Setenv("PATH", t.TempDir())
			s.start(rec, req)
			if rec.Code != tc.want || s.snapshot().Active {
				t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestInstallRedirectsChosenAddress(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "nft.log")
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$*\" >> %q\n", log)
	if err := os.WriteFile(filepath.Join(dir, "nft"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := install("172.16.1.135"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "ip saddr 172.16.1.135 tcp dport") {
		t.Errorf("missing redirect for chosen address in %s", data)
	}
}

func TestCAReadNeverReturnsKey(t *testing.T) {
	s := &Service{Dir: t.TempDir()}
	rec := httptest.NewRecorder()
	s.ca(rec, httptest.NewRequest(http.MethodGet, "/ca", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("no CA: %d", rec.Code)
	}
}

func TestPublicCAOnlyServesCertificate(t *testing.T) {
	s := &Service{Dir: t.TempDir()}
	get := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		s.PublicCA(rec, httptest.NewRequest(http.MethodGet, "/download/inspection-ca.crt", nil))
		return rec
	}
	if rec := get(); rec.Code != http.StatusNotFound {
		t.Fatalf("missing CA: %d", rec.Code)
	}
	const cert = "-----BEGIN CERTIFICATE-----\npublic\n-----END CERTIFICATE-----\n"
	if err := os.WriteFile(filepath.Join(s.Dir, "mitmproxy-ca-cert.pem"), []byte(cert), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := get()
	if rec.Code != http.StatusOK || rec.Body.String() != cert || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("CA response: %d %q %q", rec.Code, rec.Body.String(), rec.Header().Get("Cache-Control"))
	}
}
