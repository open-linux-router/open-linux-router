//go:build linux || darwin

package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// The bug this guards is a lost update, not a crash: two `olr dhcp add`
// invocations read the same stored config, each add their own item, and the
// second write silently drops the first. Neither reports an error, so the only
// evidence is a reservation that is not there days later.
func TestLockIsExclusiveAndReleases(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "dhcp.json")

	unlock, err := Lock(configPath)
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}

	// flock is held by the open file description rather than by the process, so
	// a second attempt is refused even from here.
	if _, held, err := tryLock(configPath + ".lock"); err != nil {
		t.Fatalf("tryLock: %v", err)
	} else if held {
		t.Fatal("the apply lock was handed out twice")
	}

	unlock()

	release, held, err := tryLock(configPath + ".lock")
	if err != nil {
		t.Fatalf("tryLock after unlock: %v", err)
	}
	if !held {
		t.Fatal("the apply lock was not released")
	}
	release()
}

// The lock file sits beside the config it guards, and taking it must not
// require the config to exist — the first `olr dhcp add pool` on a fresh box
// runs before there is anything to lock.
func TestLockWorksBeforeTheConfigExists(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "sub", "dhcp.json")

	unlock, err := Lock(configPath)
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}
	defer unlock()

	if _, err := os.Stat(configPath + ".lock"); err != nil {
		t.Errorf("lock file was not created: %v", err)
	}
}

// An empty path is the "no config store to guard" case, and must not fail.
func TestLockWithoutAConfigPath(t *testing.T) {
	unlock, err := Lock("")
	if err != nil {
		t.Fatalf("Lock(\"\"): %v", err)
	}
	unlock()
}
