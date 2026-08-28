package core

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestListenUnixCreatesTheSocketWithARestrictiveMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "olrd.sock")

	l, err := ListenUnix(path)
	if err != nil {
		t.Fatalf("ListenUnix: %v", err)
	}
	defer l.Close()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("socket was not created at %s: %v", path, err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		t.Fatal("the path is not a socket")
	}
	// Bound at a temporary name and renamed into place precisely so this is
	// never briefly world-writable under a default umask.
	if perm := info.Mode().Perm(); perm != SocketMode.Perm() {
		t.Errorf("socket mode = %o, want %o", perm, SocketMode.Perm())
	}
}

func TestListenUnixRemovesTheSocketOnClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "olrd.sock")

	l, err := ListenUnix(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("the socket file outlived the listener")
	}
}

// The two cases look identical on disk and must not be conflated: clearing a
// live socket would silently steal the API from a running olrd.
func TestListenUnixRefusesWhenAnotherDaemonIsListening(t *testing.T) {
	path := filepath.Join(t.TempDir(), "olrd.sock")

	first, err := ListenUnix(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()

	// Something has to be accepting, or the liveness probe cannot tell.
	go func() {
		for {
			c, err := first.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()

	if _, err := ListenUnix(path); err == nil {
		t.Fatal("a second listener bound over a live socket")
	}
}

func TestListenUnixClearsASocketLeftByAnUncleanShutdown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "olrd.sock")

	// A socket file with nobody behind it, as an unclean exit leaves.
	stale, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	if ul, ok := stale.(*net.UnixListener); ok {
		ul.SetUnlinkOnClose(false)
	}
	stale.Close()

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the stale socket was not left behind: %v", err)
	}

	l, err := ListenUnix(path)
	if err != nil {
		t.Fatalf("ListenUnix refused a stale socket: %v", err)
	}
	l.Close()
}

func TestListenUnixRefusesAPathThatIsNotASocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "olrd.sock")
	if err := os.WriteFile(path, []byte("not a socket"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Removing an ordinary file to bind over it would be destroying data we
	// were never asked to touch.
	if _, err := ListenUnix(path); err == nil {
		t.Error("bound over a regular file")
	}
}
