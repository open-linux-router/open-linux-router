package inspection

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/devices"
)

type fakeDevices struct {
	rows     []devices.Resolved
	problems []devices.Problem
}

func (f fakeDevices) List(context.Context) ([]devices.Resolved, []devices.Problem, error) {
	return f.rows, f.problems, nil
}
func row(mac string, active bool, ips ...string) devices.Resolved {
	return devices.Resolved{MAC: mac, Presence: &devices.Presence{Active: active, IPs: ips, Sources: []devices.Source{devices.SourceARP}}}
}

func TestDeviceIPRequiresUniqueCurrentPrivateIPv4(t *testing.T) {
	const mac = "aa:bb:cc:dd:ee:01"
	cases := []struct {
		name string
		rows []devices.Resolved
		ok   bool
	}{
		{"one address", []devices.Resolved{row(mac, true, "192.168.2.10")}, true},
		{"offline", []devices.Resolved{row(mac, false, "192.168.2.10")}, false},
		{"missing", []devices.Resolved{row("aa:bb:cc:dd:ee:02", true, "192.168.2.10")}, false},
		{"shared address", []devices.Resolved{row(mac, true, "192.168.2.10"), row("aa:bb:cc:dd:ee:02", true, "192.168.2.10")}, false},
		{"dual stack", []devices.Resolved{row(mac, true, "192.168.2.10", "fd00::10", "fe80::10")}, true},
		{"two addresses", []devices.Resolved{row(mac, true, "192.168.2.10", "192.168.2.11")}, false},
		{"IPv6 only", []devices.Resolved{row(mac, true, "fd00::10")}, false},
		{"two IPv4 plus IPv6", []devices.Resolved{row(mac, true, "192.168.2.10", "192.168.2.11", "fd00::10")}, false},
		{"public", []devices.Resolved{row(mac, true, "8.8.8.8")}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &Service{Devices: fakeDevices{rows: tc.rows}}
			ip, err := s.deviceIP(context.Background(), mac)
			if tc.ok != (err == nil) {
				t.Fatalf("ip=%q err=%v", ip, err)
			}
		})
	}
	s := &Service{Devices: fakeDevices{rows: cases[0].rows, problems: []devices.Problem{{Message: "source unavailable"}}}}
	if _, err := s.deviceIP(context.Background(), mac); err == nil {
		t.Fatal("accepted incomplete identity")
	}
}

func TestStartRejectsUnknownDeviceWithoutStartingProxy(t *testing.T) {
	s := &Service{Devices: fakeDevices{}, Enabled: true}
	req := httptest.NewRequest(http.MethodPost, "/session", strings.NewReader(`{"mac":"aa:bb:cc:dd:ee:01"}`))
	rec := httptest.NewRecorder()
	s.start(rec, req)
	if rec.Code != http.StatusConflict || s.snapshot().Active {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
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
