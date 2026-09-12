//go:build linux

package dial

import (
	"net"
	"syscall"

	"golang.org/x/sys/unix"
)

// bindToDevice pins a dialer's sockets to one interface.
//
// `SO_BINDTODEVICE` rather than `gateway.DialThrough`'s `SO_MARK`, and the
// difference is architectural rather than technical. Marking a socket routes it
// into an *exit*'s table, which would be the better answer here — a reflector
// asked through the exit whose address is being published gives the address
// that exit actually has. But `dial` is a foundation module and `gateway` is a
// service module, and design.md §4.1 forbids the arrow pointing that way. So v1
// binds by interface, which is what docs/ddns.md §3.3 specifies anyway, and
// per-exit binding waits for a fact subscription in the direction the graph
// allows.
//
// It needs CAP_NET_RAW, which olrd.service already grants.
func bindToDevice(d *net.Dialer, iface string) error {
	d.Control = func(_, _ string, c syscall.RawConn) error {
		var setErr error
		err := c.Control(func(fd uintptr) {
			setErr = unix.SetsockoptString(int(fd), unix.SOL_SOCKET, unix.SO_BINDTODEVICE, iface)
		})
		if err != nil {
			return err
		}
		return setErr
	}
	return nil
}
