package iptv

import (
	"context"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

type testLinks struct{}

func (testLinks) Interface(name string) (bool, bool, bool, error) {
	return name == "iptv0", name == "iptv0", name == "iptv0", nil
}
func (testLinks) Interfaces() ([]string, error) {
	return []string{"iptv0", "lan0", "lo", "other0"}, nil
}
func (testLinks) Networks() ([]Network, error) {
	return []Network{{Name: "lan", Members: []string{"lan0"}, IPv4: true}, {Name: "bad", Members: []string{"lan1"}, IPv4: false}}, nil
}
func TestResolveAndRender(t *testing.T) {
	cfg := Config{Enabled: true, Upstream: "iptv0", Networks: []string{"lan"}, Sources: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}}
	got, e := Resolve(cfg, testLinks{})
	if e != nil {
		t.Fatal(e)
	}
	text := string(Render(got))
	for _, part := range []string{"phyint iptv0 upstream", "altnet 10.0.0.0/8", "phyint lan0 downstream", "phyint lo disabled", "phyint other0 disabled"} {
		if !strings.Contains(text, part) {
			t.Errorf("missing %q: %s", part, text)
		}
	}
	cases := []Config{
		{Enabled: true, Upstream: "lan0", Networks: []string{"lan"}},
		{Enabled: true, Upstream: "iptv0", Networks: []string{"bad"}},
		{Enabled: true, Upstream: "iptv0", Networks: []string{"lan", "lan"}},
		{Enabled: true, Upstream: "iptv0", Networks: []string{"missing"}},
		{Enabled: true, Upstream: "iptv0", Networks: []string{"lan"}, Sources: []netip.Prefix{netip.MustParsePrefix("224.0.0.0/4")}},
	}
	for _, bad := range cases {
		if _, e := Resolve(bad, testLinks{}); e == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	if _, e := UnmarshalConfig([]byte(`{"enabled":true,"oops":1}`)); e == nil {
		t.Error("unknown key accepted")
	}
	if _, e := UnmarshalConfig([]byte(`{} {}`)); e == nil {
		t.Error("trailing JSON accepted")
	}
}

type testUnit struct {
	active, enabled         bool
	starts, restarts, stops int
}

func (u *testUnit) Status(context.Context) (core.UnitStatus, error) {
	return core.UnitStatus{Active: u.active, Enabled: u.enabled}, nil
}
func (u *testUnit) Start(context.Context) error   { u.active = true; u.starts++; return nil }
func (u *testUnit) Stop(context.Context) error    { u.active = false; u.stops++; return nil }
func (u *testUnit) Restart(context.Context) error { u.restarts++; return nil }
func (u *testUnit) Reload(context.Context) error  { return nil }
func (u *testUnit) Enable(context.Context) error  { u.enabled = true; return nil }
func (u *testUnit) Disable(context.Context) error { u.enabled = false; return nil }
func TestApplyIdempotentAndDisable(t *testing.T) {
	root := t.TempDir()
	u := &testUnit{}
	store := core.NewStore(filepath.Join(root, "olr.json"), ModuleName)
	a := Applier{Store: store, Links: testLinks{}, Unit: u, Root: root}
	cfg := Config{Enabled: true, Upstream: "iptv0", Networks: []string{"lan"}}
	if _, e := a.Apply(context.Background(), cfg, true); e != nil {
		t.Fatal(e)
	}
	if u.starts != 1 || !u.enabled {
		t.Fatalf("unit: %+v", u)
	}
	if _, e := a.Apply(context.Background(), cfg, true); e != nil {
		t.Fatal(e)
	}
	if u.starts != 1 || u.restarts != 0 {
		t.Fatalf("idempotency: %+v", u)
	}
	if e := os.WriteFile(a.path(), []byte("modified"), 0644); e != nil {
		t.Fatal(e)
	}
	if _, e := a.Apply(context.Background(), cfg, false); e != nil {
		t.Fatal(e)
	}
	if u.restarts != 1 {
		t.Fatal("drift not repaired")
	}
	cfg.Enabled = false
	if _, e := a.Apply(context.Background(), cfg, true); e != nil {
		t.Fatal(e)
	}
	if u.active || u.enabled || u.stops != 1 {
		t.Fatalf("not stopped: %+v", u)
	}
	saved, e := a.Load()
	if e != nil || saved.Enabled {
		t.Fatalf("saved %+v: %v", saved, e)
	}
}
