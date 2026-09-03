//go:build linux

package routing

import (
	"context"
	"syscall"

	"golang.org/x/sys/unix"
)

// DialThrough opens a TCP connection routed through one exit.
//
// The mechanism is `SO_MARK` on the socket, and it works because the RPDB has
// no notion of forwarded versus locally generated: the `ip rule` this module
// installs matches an fwmark whatever set it, so a locally originated socket
// carrying an exit's mark is routed into that exit's table exactly as a
// forwarded packet would be.
//
// That is worth stating because §3.5 says the opposite about *classification* —
// we mark forwarded traffic only, and the router's own traffic follows `main`.
// Both are true. The classifier never touches this socket; we set the mark on
// it by hand, which is the one place olr deliberately routes its own traffic
// through an exit, and it is the only way to test the path rather than the box
// at the end of it.
//
// TCP rather than ICMP, for the reason the whole check exists: a proxy box
// answers pings from its own kernel while its userspace forwards nothing. A
// completed handshake with something on the far side is the weakest claim that
// still means the exit works.
func DialThrough(ctx context.Context, mark uint32, target string) error {
	d := dialer
	d.Control = func(_, _ string, c syscall.RawConn) error {
		var setErr error
		err := c.Control(func(fd uintptr) {
			setErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_MARK, int(mark))
		})
		if err != nil {
			return err
		}
		return setErr
	}

	conn, err := d.DialContext(ctx, "tcp", target)
	if err != nil {
		return err
	}
	// Closed straight away. We are asking whether the path works, not talking
	// to whatever is on the end of it, and holding the connection open would
	// leave one socket per exit per interval on somebody else's server.
	return conn.Close()
}
