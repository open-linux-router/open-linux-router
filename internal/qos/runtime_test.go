package qos

import (
	"github.com/open-linux-router/open-linux-router/internal/runtimedb"
	"os"
	"path/filepath"
	"testing"
)

func TestSQLiteOwnershipIsBootScoped(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "qos.json")
	if err := os.WriteFile(legacy, []byte(`{"queues":[{"interface":"eth0","ifb":"olrq1"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	db, err := runtimedb.Open(filepath.Join(dir, "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	e := Engine{Runtime: db, BootID: "boot-a", StatePath: legacy}
	s, err := e.readState()
	if err != nil || len(s.Queues) != 1 {
		t.Fatal("ownership import failed", err)
	}
	if err := e.saveState(runtimeState{}); err != nil {
		t.Fatal(err)
	}
	s, err = e.readState()
	if err != nil || len(s.Queues) != 0 {
		t.Fatal("old ownership resurrected", err)
	}
	e.BootID = "boot-b"
	s, err = e.readState()
	if err != nil || len(s.Queues) != 0 {
		t.Fatal("previous boot ownership restored", err)
	}
}
