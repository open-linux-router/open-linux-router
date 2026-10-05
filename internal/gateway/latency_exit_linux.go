//go:build linux

package gateway

import (
	"context"
	"net"
	"syscall"

	"golang.org/x/sys/unix"
)

func markedLatencyDial(mark uint32) func(context.Context, string, string) (net.Conn, error) {
	d := net.Dialer{Control: func(_, _ string, c syscall.RawConn) error {
		var setErr error
		if err := c.Control(func(fd uintptr) { setErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_MARK, int(mark)) }); err != nil {
			return err
		}
		return setErr
	}}
	return d.DialContext
}
