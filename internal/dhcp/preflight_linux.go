//go:build linux

package dhcp

import (
	"fmt"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// dhcpServerPort is the UDP port a DHCPv4 server listens on.
const dhcpServerPort = 67

// PortConflict reports whether something already holds the DHCP server port.
//
// We start our own dnsmasq instance rather than taking over the distro's
// (design.md §3.4), which means the operator can end up with two DHCP servers
// racing for the same port. Rather than declaring Conflicts= in the unit and
// stopping their daemon — machine-wide interference of exactly the kind §3.4
// forbids — we look first and refuse with an explanation.
//
// The procfs scan itself is core's, because the dns module needs the identical
// check for :53 and a second copy is a second place to fix a parsing bug. What
// stays here is the port and the refusal text: only this module can name
// dnsmasq and the command that finds the incumbent.
func PortConflict() (bool, error) { return core.UDPPortInUse(dhcpServerPort) }

// ErrPortInUse explains a refused start.
func ErrPortInUse() error {
	return fmt.Errorf(
		"UDP/%d is already in use, so another DHCP server is running on this box.\n"+
			"olr runs its own dnsmasq instance and will not stop somebody else's daemon.\n"+
			"Find the holder with `ss -lunp sport = :%d` and stop it, or leave DHCP to it",
		dhcpServerPort, dhcpServerPort)
}
