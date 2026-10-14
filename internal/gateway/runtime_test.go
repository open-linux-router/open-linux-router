package gateway

import (
	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/runtimedb"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestActivitySQLiteImportAndBaseline(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "activity.json")
	a, err := OpenActivity(legacy)
	if err != nil {
		t.Fatal(err)
	}
	addr := netip.MustParseAddr("192.0.2.1")
	at := time.Now().Truncate(30 * time.Minute)
	a.observe(at, []Usage{{Addr: addr, UpBytes: 100}}, true)
	a.observe(at.Add(time.Second), []Usage{{Addr: addr, UpBytes: 120}}, true)
	if err := a.Save(); err != nil {
		t.Fatal(err)
	}
	db, err := runtimedb.Open(filepath.Join(dir, "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	b, err := OpenActivityDB(db, legacy)
	if err != nil {
		t.Fatal(err)
	}
	b.observe(at.Add(2*time.Second), []Usage{{Addr: addr, UpBytes: 900}}, true)
	b.observe(at.Add(3*time.Second), []Usage{{Addr: addr, UpBytes: 910}}, true)
	if err := b.Save(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := OpenActivityDB(db, legacy)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.snapshot([]netip.Addr{addr}); len(got) != 1 || got[0].Bytes.Up != 30 {
		t.Fatalf("history %+v", got)
	}
}

func TestLatencyConfigMigrationClearAndRename(t *testing.T) {
	store := core.NewStore(filepath.Join(t.TempDir(), "olr.json"), ModuleName)
	legacy := store.Path() + ".latency-sites"
	if err := os.WriteFile(legacy, []byte(`[{"name":"Example","url":"https://example.org"}]`), 0600); err != nil {
		t.Fatal(err)
	}
	m, err := OpenCustomLatencyConfig(store)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Config()) != 1 {
		t.Fatal("targets not imported")
	}
	if err := m.Replace(nil); err != nil {
		t.Fatal(err)
	}
	m, err = OpenCustomLatencyConfig(store)
	if err != nil || len(m.Config()) != 0 {
		t.Fatal("cleared targets resurrected", err)
	}
	c := Config{Exits: []Exit{{Name: "old"}}, LatencySites: []CustomLatencySite{{Name: "Example", URL: "https://example.org", Exit: "old"}}}
	if !c.Rename("old", "new") || c.LatencySites[0].Exit != "new" {
		t.Fatal("rename did not cascade")
	}
}
