//go:build linux

package core

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// The inode column is what the whole lookup hangs on, so the fixtures carry
// real rows rather than a simplified shape: 0x0035 is 53, and 34567 is the
// socket inode that has to be matched against a process's open descriptors.

// fakeProc builds a /proc tree: one process holding the given socket inode,
// plus a second that holds nothing, so a passing test is not just finding the
// only directory present.
func fakeProc(t *testing.T, pid int, comm, cgroup, inode string) string {
	t.Helper()
	root := t.TempDir()

	// A non-process entry, which /proc is full of, and a process with an
	// unrelated socket. Both must be walked past without incident.
	if err := os.MkdirAll(filepath.Join(root, "net"), 0o755); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(root, "999", "fd")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("socket:[11111]", filepath.Join(other, "3")); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(root, strconv.Itoa(pid))
	if err := os.MkdirAll(filepath.Join(dir, "fd"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Descriptor 0 is not a socket at all; the walk must not stop on it.
	if err := os.Symlink("/dev/null", filepath.Join(dir, "fd", "0")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("socket:["+inode+"]", filepath.Join(dir, "fd", "7")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "comm"), []byte(comm+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cgroup"), []byte(cgroup), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func writeNet(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "udp")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPortHolderNamesTheProcessAndItsUnit(t *testing.T) {
	root := fakeProc(t, 3712, "dnsmasq", "0::/system.slice/dnsmasq.service\n", "34567")
	net := writeNet(t, procNetUDP)

	holder, found := portHolder(root, []string{net}, 53)
	if !found {
		t.Fatal("portHolder found nothing for port 53")
	}
	if holder.PID != 3712 || holder.Name != "dnsmasq" || holder.Unit != "dnsmasq.service" {
		t.Errorf("got %+v; want pid 3712, dnsmasq, dnsmasq.service", holder)
	}
	if want := "dnsmasq (pid 3712, dnsmasq.service)"; holder.String() != want {
		t.Errorf("String() = %q; want %q", holder.String(), want)
	}
}

// A port nothing holds must report nothing, not the first process it walks
// past. 67 is absent from this fixture.
func TestPortHolderReportsNothingForAFreePort(t *testing.T) {
	root := fakeProc(t, 3712, "dnsmasq", "0::/system.slice/dnsmasq.service\n", "34567")
	if holder, found := portHolder(root, []string{writeNet(t, procNetUDP)}, 67); found {
		t.Errorf("portHolder(67) = %+v, true; want not found", holder)
	}
}

// The socket is in the table but no process admits to it — the ordinary result
// of running this as a non-root user. It must degrade to "cannot tell" rather
// than inventing a holder, because the caller prints a different sentence.
func TestPortHolderDegradesWhenNoProcessMatches(t *testing.T) {
	root := fakeProc(t, 3712, "dnsmasq", "0::/system.slice/dnsmasq.service\n", "99999")
	if holder, found := portHolder(root, []string{writeNet(t, procNetUDP)}, 53); found {
		t.Errorf("portHolder = %+v, true; want not found", holder)
	}
}

func TestPortHolderSurvivesAnUnreadableProcRoot(t *testing.T) {
	net := writeNet(t, procNetUDP)
	if _, found := portHolder(filepath.Join(t.TempDir(), "absent"), []string{net}, 53); found {
		t.Error("want not found when /proc cannot be read")
	}
}

// unitOf and Holder.String are tested in portholder_test.go, which carries no
// build tag: they are what an operator reads, and a message that renders only
// on Linux is the bug this whole file exists to have caught.
