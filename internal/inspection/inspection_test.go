package inspection

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
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
	return devices.Resolved{MAC: mac, Presence: &devices.Presence{Active: active, IPs: ips, NeighborIPs: ips, Sources: []devices.Source{devices.SourceARP}}}
}

func TestDeviceIPsRequiresUniqueCurrentPrivateIPv4(t *testing.T) {
	const mac = "aa:bb:cc:dd:ee:01"
	cases := []struct {
		name string
		rows []devices.Resolved
		want []string
	}{
		{"one address", []devices.Resolved{row(mac, true, "192.168.2.10")}, []string{"192.168.2.10"}},
		{"offline", []devices.Resolved{row(mac, false, "192.168.2.10")}, nil},
		{"missing", []devices.Resolved{row("aa:bb:cc:dd:ee:02", true, "192.168.2.10")}, nil},
		{"shared address", []devices.Resolved{row(mac, true, "192.168.2.10"), row("aa:bb:cc:dd:ee:02", true, "192.168.2.10")}, nil},
		{"dual stack", []devices.Resolved{row(mac, true, "192.168.2.10", "fd00::10", "fe80::10")}, []string{"192.168.2.10"}},
		{"two addresses", []devices.Resolved{row(mac, true, "192.168.2.11", "192.168.2.10")}, []string{"192.168.2.10", "192.168.2.11"}},
		{"IPv6 only", []devices.Resolved{row(mac, true, "fd00::10")}, nil},
		{"two IPv4 plus IPv6", []devices.Resolved{row(mac, true, "192.168.2.10", "192.168.2.11", "fd00::10")}, []string{"192.168.2.10", "192.168.2.11"}},
		{"public", []devices.Resolved{row(mac, true, "8.8.8.8")}, nil},
		{"mixed public", []devices.Resolved{row(mac, true, "192.168.2.10", "8.8.8.8")}, nil},
		{"invalid", []devices.Resolved{row(mac, true, "not-an-ip")}, nil},
		{"lease only", []devices.Resolved{{MAC: mac, Presence: &devices.Presence{Active: true, IPs: []string{"192.168.2.10"}}}}, nil},
		{"old lease and active ARP", []devices.Resolved{{MAC: mac, Presence: &devices.Presence{Active: true, IPs: []string{"192.168.2.10", "192.168.2.11"}, NeighborIPs: []string{"192.168.2.11"}}}}, []string{"192.168.2.11"}},
		{"other device's old lease", []devices.Resolved{row(mac, true, "172.16.1.135"), {MAC: "aa:bb:cc:dd:ee:02", Presence: &devices.Presence{Active: true, IPs: []string{"172.16.1.135"}}}}, []string{"172.16.1.135"}},
		{"other device's inactive ARP", []devices.Resolved{row(mac, true, "172.16.1.135"), {MAC: "aa:bb:cc:dd:ee:02", Presence: &devices.Presence{Active: false, IPs: []string{"172.16.1.135"}}}}, []string{"172.16.1.135"}},
		{"other device's active ARP", []devices.Resolved{row(mac, true, "172.16.1.135"), row("aa:bb:cc:dd:ee:02", true, "172.16.1.135")}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &Service{Devices: fakeDevices{rows: tc.rows}}
			ips, err := s.deviceIPs(context.Background(), mac)
			if (tc.want == nil) != (err != nil) || !slices.Equal(ips, tc.want) {
				t.Fatalf("ips=%q err=%v, want %q", ips, err, tc.want)
			}
		})
	}
	s := &Service{Devices: fakeDevices{rows: cases[0].rows, problems: []devices.Problem{{Message: "source unavailable"}}}}
	if _, err := s.deviceIPs(context.Background(), mac); err == nil {
		t.Fatal("accepted incomplete identity")
	}
	s = &Service{Devices: fakeDevices{rows: cases[3].rows}}
	if _, err := s.deviceIPs(context.Background(), mac); err == nil || !strings.Contains(err.Error(), "conflicting current neighbour observations") {
		t.Fatalf("shared active address: %v", err)
	}
}

func TestDeviceIPsIgnoresReassignedLease(t *testing.T) {
	const current = "aa:bb:cc:dd:ee:01"
	rows, problems := devices.Build(devices.Config{}, []devices.Sighting{
		{MAC: current, IP: "172.16.1.135", Source: devices.SourceARP, Active: true},
		{MAC: "aa:bb:cc:dd:ee:02", IP: "172.16.1.135", Source: devices.SourceDHCPLease, Active: true},
	}, nil, nil)
	if len(problems) != 0 {
		t.Fatalf("presence problems: %v", problems)
	}
	s := &Service{Devices: fakeDevices{rows: rows}}
	ips, err := s.deviceIPs(context.Background(), current)
	if err != nil || !slices.Equal(ips, []string{"172.16.1.135"}) {
		t.Fatalf("ips=%v err=%v", ips, err)
	}
}

func TestInstallRedirectsEveryAddress(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "nft.log")
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$*\" >> %q\n", log)
	if err := os.WriteFile(filepath.Join(dir, "nft"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := install([]string{"192.168.2.10", "192.168.2.11"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	for _, ip := range []string{"192.168.2.10", "192.168.2.11"} {
		if !strings.Contains(string(data), "ip saddr "+ip+" tcp dport") {
			t.Errorf("missing redirect for %s in %s", ip, data)
		}
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
