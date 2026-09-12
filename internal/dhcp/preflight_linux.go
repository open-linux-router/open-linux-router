//go:build linux

package dhcp

import (
	"github.com/open-linux-router/open-linux-router/internal/core"
)

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
// stays in this module is the port, and the refusal text in preflight.go: only
// this module can name dnsmasq and say what to do about it.
func PortConflict() (bool, error) { return core.UDPPortInUse(dhcpServerPort) }
