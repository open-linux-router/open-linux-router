//go:build linux

package inspection

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
)

func TestOrphanPIDsMatchesOnlyThisRoutersProxy(t *testing.T) {
	root := t.TempDir()
	dir := "/var/lib/open-linux-router/inspection"
	write := func(pid, cmdline string) {
		t.Helper()
		path := filepath.Join(root, pid)
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "cmdline"), []byte(cmdline), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("101", "mitmweb\x00--mode\x00transparent\x00--set\x00confdir="+dir+"\x00")
	write("102", "mitmweb\x00--set\x00confdir=/somewhere/else\x00")
	write("103", "caddy\x00--set\x00confdir="+dir+"\x00")
	write("not-a-pid", "mitmweb\x00--set\x00confdir="+dir+"\x00")

	got := orphanPIDs(root, dir)
	if want := []int{101}; !slices.Equal(got, want) {
		t.Fatalf("orphans = %v, want %v", got, want)
	}
	if got := orphanPIDs(root, ""); got != nil {
		t.Fatalf("empty dir matched %v", got)
	}
}

func TestOrphanPIDsSkipsItself(t *testing.T) {
	root := t.TempDir()
	self := strconv.Itoa(os.Getpid())
	dir := filepath.Join(root, "inspection")
	if err := os.MkdirAll(filepath.Join(root, self), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, self, "cmdline"), []byte("mitmweb\x00confdir="+dir+"\x00"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := orphanPIDs(root, dir); len(got) != 0 {
		t.Fatalf("matched the test process: %v", got)
	}
}
