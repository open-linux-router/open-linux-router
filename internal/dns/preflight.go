package dns

import (
	"fmt"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// DNSPort is where a resolver listens.
const DNSPort = 53

// PortConflict reports whether something already holds the DNS port.
//
// The same refusal internal/dhcp makes for UDP/67, and for the same reason: olr
// runs its own resolver rather than taking over the distro's (design.md §3.4),
// so an operator can end up with two things racing for one socket. On this port
// the incumbent is usually systemd-resolved, which is installed and listening
// on a great many Debian boxes without anybody having chosen it — so this check
// is the difference between a clear message and a relay that flaps.
//
// Both protocols, unlike dhcp's: DNS is served over UDP and TCP, and a resolver
// holding only the TCP half still breaks every response too large for a
// datagram.
func PortConflict() (bool, error) {
	if inUse, err := core.UDPPortInUse(DNSPort); err != nil || inUse {
		return inUse, err
	}
	return core.TCPPortInUse(DNSPort)
}

// ErrPortInUse explains a refused start.
//
// It names systemd-resolved because that is nearly always the answer, and
// because the fix is not obvious: the service has to be told to stop listening,
// not merely stopped, or it comes back at the next boot and DNS breaks then
// instead — at the least convenient possible moment.
func ErrPortInUse() error {
	return fmt.Errorf(
		"port %d is already in use, so something else on this box is serving DNS.\n"+
			"olr runs its own resolver and will not stop somebody else's daemon.\n"+
			"On a systemd distribution this is almost always systemd-resolved. "+
			"Find the holder with `ss -lunp sport = :%[1]d` and `ss -ltnp sport = :%[1]d`.\n"+
			"To hand DNS to olr, set DNSStubListener=no in /etc/systemd/resolved.conf "+
			"and restart systemd-resolved — stopping it alone will not survive a reboot",
		DNSPort)
}

// ListensOnDefaultPort reports whether any listen address uses port 53.
//
// It gates the conflict check, because that check asks about 53 and nothing
// else. An operator who moved the relay to another port has taken
// responsibility for that port; refusing to start over a conflict we never
// looked for would be worse than letting the journal say so.
func ListensOnDefaultPort(c Config) bool {
	for _, l := range c.Listen {
		if l.Port() == DNSPort {
			return true
		}
	}
	return false
}
