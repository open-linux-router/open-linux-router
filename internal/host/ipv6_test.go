package host

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIPv6AutoDisableEnableAndRestore(t *testing.T) {
	a := Applier{Root: t.TempDir()}
	for key, value := range map[string]string{"accept_ra": "1\n", "autoconf": "1\n"} {
		path := a.ipv6Key("eth0", key)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(value), 0644); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		settings map[string]bool
		ra, auto string
	}{
		{map[string]bool{"eth0": false}, "0", "0"},
		{map[string]bool{"eth0": true}, "2", "1"},
		{nil, "1", "1"},
	} {
		for _, step := range a.applyIPv6Auto(tc.settings) {
			if step.Error != "" {
				t.Fatal(step.Error)
			}
		}
		for key, want := range map[string]string{"accept_ra": tc.ra, "autoconf": tc.auto} {
			got, err := os.ReadFile(a.ipv6Key("eth0", key))
			if err != nil || strings.TrimSpace(string(got)) != want {
				t.Fatalf("%s = %q, %v; want %s", key, got, err, want)
			}
		}
	}
}

func TestIPv6AutoDhcpcdPreservesIPv4(t *testing.T) {
	for _, on := range []bool{false, true} {
		got := renderDhcpcd(stockDhcpcd, Desired{IPv6Auto: map[string]bool{"eth0": on}})
		want := "ipv6"
		if !on {
			want = "noipv6"
		}
		if !strings.Contains(got, "interface eth0\n\t"+want+"\n") || strings.Contains(got, "noipv4") {
			t.Fatal(got)
		}
		if renderDhcpcd(got, Desired{}) != stockDhcpcd {
			t.Fatal("host configuration was not restored")
		}
	}
}

func TestIPv6AutoRefusesAnotherManagerBeforeChangingTheHost(t *testing.T) {
	a := Applier{Root: t.TempDir(), Index: func(string) (int, error) { return 2, nil }}
	path := a.path("/run/NetworkManager/devices/2")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("managed=true\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := a.CheckIPv6Auto(map[string]bool{"eth0": false}); err == nil {
		t.Fatal("another manager was accepted")
	}
}
