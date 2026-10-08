package socksout

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

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
func TestApplyLifecycle(t *testing.T) {
	root := t.TempDir()
	u := &testUnit{}
	a := Applier{Store: core.NewStore(filepath.Join(root, "olr.json"), ModuleName), Unit: u, Root: root}
	ctx := context.Background()
	c := Config{Enabled: true, Proxy: "socks5://192.0.2.10:1080"}
	if _, err := a.Apply(ctx, c, true); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(a.path())
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode: %v %v", info, err)
	}
	if u.starts != 1 || !u.enabled {
		t.Fatalf("unit after start: %+v", u)
	}
	if _, err := a.Apply(ctx, c, false); err != nil {
		t.Fatal(err)
	}
	if u.starts != 1 || u.restarts != 0 {
		t.Fatalf("idempotency: %+v", u)
	}
	c.Proxy = "socks5://192.0.2.11:1080"
	if _, err := a.Apply(ctx, c, true); err != nil {
		t.Fatal(err)
	}
	if u.restarts != 1 {
		t.Fatalf("unit after edit: %+v", u)
	}
	c.Enabled = false
	if _, err := a.Apply(ctx, c, true); err != nil {
		t.Fatal(err)
	}
	if u.active || u.enabled || u.stops != 1 {
		t.Fatalf("unit after disable: %+v", u)
	}
	saved, err := a.Load()
	if err != nil || saved.Enabled {
		t.Fatalf("saved: %+v %v", saved, err)
	}
}

func TestPlanDoesNotStoreOrStart(t *testing.T) {
	root := t.TempDir()
	u := &testUnit{}
	a := Applier{Store: core.NewStore(filepath.Join(root, "olr.json"), ModuleName), Unit: u, Root: root}
	p, err := a.Plan(context.Background(), Config{Enabled: true, Proxy: "socks5://192.0.2.10:1080"})
	if err != nil || p.Empty || u.starts != 0 {
		t.Fatalf("plan: %+v, %v; unit: %+v", p, err, u)
	}
	if _, err := os.Stat(a.path()); !os.IsNotExist(err) {
		t.Fatalf("plan wrote config: %v", err)
	}
}
