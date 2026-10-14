package host

import (
	"github.com/open-linux-router/open-linux-router/internal/runtimedb"
	"os"
	"path/filepath"
	"testing"
)

func TestRecoverySQLiteDoesNotReimportDeletedBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "resolv.conf.orig")
	if err := os.WriteFile(path, []byte("nameserver 192.0.2.1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	db, err := runtimedb.Open(filepath.Join(dir, "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a := Applier{Runtime: db}
	data, err := a.readRecovery("host:resolv", path)
	if err != nil || len(data) == 0 {
		t.Fatal("backup not imported", err)
	}
	if err := a.deleteRecovery("host:resolv", path); err != nil {
		t.Fatal(err)
	}
	data, err = a.readRecovery("host:resolv", path)
	if err != nil || data != nil {
		t.Fatal("backup resurrected", err)
	}
	db.Close()
	if _, err := a.readRecovery("host:resolv", path); err == nil {
		t.Fatal("unavailable database treated as absent originals")
	}
}
