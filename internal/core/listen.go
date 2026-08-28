package core

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

// The API is served over a unix socket, with an optional TCP listener for
// remote access and the WebUI (design.md §6.2).
//
// The two are not equivalent and are not meant to be. The socket is the local
// admin path: it is reachable only by whoever can open a file in /run/olr, so
// filesystem permissions *are* its authentication and no token is involved. TCP
// has no such property and is always authenticated (see auth.go).

// DefaultSocket matches cli.DefaultSocket. Duplicated as a constant rather than
// imported because core must not depend on the CLI; the socket_test asserts
// they agree.
const DefaultSocket = "/run/olr/olrd.sock"

// SocketMode is the unix socket's permission bits.
//
// 0660 leaves room for an `olr` group to administer the router without being
// root. That group does not exist yet — packaging has to create it and the
// socket has to be chowned to it — so today, with olrd running as root, the
// effective access is root-only. The mode is the half of the arrangement that
// belongs in the code; the group is the half that belongs in the .deb.
const SocketMode os.FileMode = 0o660

// ListenUnix listens on a unix socket at path.
//
// It refuses to start if another process is already listening there, and
// otherwise clears a socket left behind by an unclean shutdown. Those two cases
// look identical on disk and must not be conflated: removing a live socket
// would silently steal the API from a running olrd.
func ListenUnix(path string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}

	if info, err := os.Stat(path); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("%s exists and is not a socket", path)
		}
		// A successful connect means somebody is serving. Anything else means
		// the file is a leftover and is ours to clear.
		if c, err := net.DialTimeout("unix", path, time.Second); err == nil {
			c.Close()
			return nil, fmt.Errorf("another olrd is already listening on %s", path)
		}
		if err := os.Remove(path); err != nil {
			return nil, fmt.Errorf("removing stale socket %s: %w", path, err)
		}
	}

	// Bound at a temporary name, permissioned, then moved into place. Listening
	// directly on path would leave a window in which the socket exists with
	// whatever the umask allowed — briefly world-writable on a default umask,
	// which on an admin API is not a window worth having.
	tmp := path + ".tmp"
	_ = os.Remove(tmp)

	l, err := net.Listen("unix", tmp)
	if err != nil {
		return nil, fmt.Errorf("listening on %s: %w", path, err)
	}
	if ul, ok := l.(*net.UnixListener); ok {
		// Go would otherwise unlink the temporary name on Close, which is not
		// where the socket lives by then. Cleanup is unixListener's job below.
		ul.SetUnlinkOnClose(false)
	}

	if err := os.Chmod(tmp, SocketMode); err != nil {
		l.Close()
		_ = os.Remove(tmp)
		return nil, fmt.Errorf("setting mode on %s: %w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		l.Close()
		_ = os.Remove(tmp)
		return nil, fmt.Errorf("moving socket into place at %s: %w", path, err)
	}

	return &unixListener{Listener: l, path: path}, nil
}

// unixListener removes the socket file on Close, since the rename above took
// that responsibility away from net.UnixListener.
type unixListener struct {
	net.Listener
	path string
}

func (u *unixListener) Close() error {
	err := u.Listener.Close()
	if rmErr := os.Remove(u.path); rmErr != nil && !os.IsNotExist(rmErr) && err == nil {
		err = rmErr
	}
	return err
}

// ListenTCP listens on addr for the WebUI and remote clients.
func ListenTCP(addr string) (net.Listener, error) {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listening on %s: %w", addr, err)
	}
	return l, nil
}

// IsLoopback reports whether addr names only a loopback interface.
//
// Used to keep --no-auth from ever being reachable off the box. An empty host
// ("":8080) binds every interface and is emphatically not loopback.
func IsLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "" {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
