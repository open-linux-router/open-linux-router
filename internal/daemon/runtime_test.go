package daemon

import (
	"github.com/open-linux-router/open-linux-router/internal/gateway"
	"github.com/open-linux-router/open-linux-router/internal/runtimedb"
	"os"
	"path/filepath"
	"testing"
)

func TestCorruptLegacyHistoryDegradesWithoutBlockingPersistence(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "activity.json")
	if err := os.WriteFile(legacy, []byte("broken json"), 0600); err != nil {
		t.Fatal(err)
	}
	db, err := runtimedb.Open(filepath.Join(dir, "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a, err := restoreHistory(db, quietLogger(), func() (*gateway.Activity, error) { return gateway.OpenActivityDB(db, legacy) })
	if err != nil {
		t.Fatal("history prevented startup", err)
	}
	if err := a.Save(); err != nil {
		t.Fatal("new history cannot persist", err)
	}
	if data, err := os.ReadFile(legacy); err != nil || string(data) != "broken json" {
		t.Fatal("source discarded")
	}
}
