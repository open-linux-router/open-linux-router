package dhcp

import (
	"fmt"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// dhcpServerPort is the UDP port a DHCPv4 server listens on.
const dhcpServerPort = 67

// ErrPortInUse explains a refused start, naming the holder when it can find it.
//
// Deliberately *not* behind a build tag, even though PortConflict is — the same
// arrangement internal/ingress uses, and for the same two reasons. The message
// is the part an operator reads, so it should read identically wherever it is
// built; and the branches below are worth testing on a laptop, which a
// linux-tagged file would prevent.
func ErrPortInUse() error {
	holder, found := core.UDPPortHolder(dhcpServerPort)
	return portInUseError(holder, found)
}

// portInUseError is the message, given what the lookup found.
//
// Naming the holder matters more here than the port number does. The likeliest
// incumbent on an olr box is the distribution's own dnsmasq — the same binary
// olr drives, under a unit olr did not write — and "another DHCP server is
// running" reads like a contradiction to somebody who knows they installed only
// one. `dnsmasq (pid 3712, dnsmasq.service)` ends that in a line, and names
// something they can disable.
func portInUseError(holder core.Holder, found bool) error {
	const preamble = "UDP/%d is already in use, so another DHCP server is running on this box.\n" +
		"olr runs its own dnsmasq instance and will not stop somebody else's daemon.\n"

	switch {
	case !found:
		return fmt.Errorf(preamble+
			"Find the holder with `ss -lunp sport = :%[1]d` and stop it, or leave DHCP to it",
			dhcpServerPort)

	case holder.Unit != "":
		return fmt.Errorf(preamble+
			"It is held by %s.\n"+
			"To hand DHCP to olr:\n"+
			"  sudo systemctl disable --now %s\n"+
			"`disable --now` rather than `stop`: a stopped unit returns at the next "+
			"boot and takes the port back before olr's does",
			dhcpServerPort, holder, holder.Unit)

	default:
		return fmt.Errorf(preamble+
			"It is held by %s, which is not running under a systemd unit — "+
			"stop it however it was started, or leave DHCP to it",
			dhcpServerPort, holder)
	}
}
