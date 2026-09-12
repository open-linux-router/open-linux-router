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

// ErrPortInUse explains a refused start, naming the holder when it can find it.
//
// This used to assert that the holder was systemd-resolved — "nearly always the
// answer" — and tell the operator to set DNSStubListener=no. That advice is
// exactly right for systemd-resolved and useless for anything else, and on an
// olr box the incumbent is frequently *dnsmasq*: our own documentation says to
// install dnsmasq, `apt install dnsmasq` gets the full Debian package rather
// than dnsmasq-base, and that package ships a service which binds :53 on
// install. Sending that operator to edit resolved.conf costs them an afternoon.
//
// So the holder is looked up and named, and the advice follows from what was
// found. The guess survives only as the fallback, and now says it is one.
func ErrPortInUse() error {
	holder, found := core.UDPPortHolder(DNSPort)
	if !found {
		holder, found = core.TCPPortHolder(DNSPort)
	}
	return portInUseError(holder, found)
}

// portInUseError is the message, given what the lookup found. Separated so the
// branches can be tested without a box that happens to have :53 taken.
func portInUseError(holder core.Holder, found bool) error {
	const preamble = "port %d is already in use, so something else on this box is serving DNS.\n" +
		"olr runs its own resolver and will not stop somebody else's daemon.\n"

	if !found {
		return fmt.Errorf(preamble+
			"Find the holder with `ss -lunp sport = :%[1]d` and `ss -ltnp sport = :%[1]d`.\n"+
			"If it is systemd-resolved, set DNSStubListener=no in /etc/systemd/resolved.conf "+
			"and restart it — stopping it alone will not survive a reboot",
			DNSPort)
	}

	switch {
	// systemd-resolved is the one holder that must not simply be disabled: it
	// is what the rest of the box resolves through, so stopping it leaves this
	// machine unable to resolve anything until olr's relay is up. Telling it to
	// give up the socket while it keeps running is the correct move, and it is
	// not a thing anybody guesses.
	case holder.Unit == "systemd-resolved.service":
		return fmt.Errorf(preamble+
			"It is held by %s.\n"+
			"Set DNSStubListener=no in /etc/systemd/resolved.conf and restart it:\n"+
			"  sudo systemctl restart systemd-resolved\n"+
			"Stopping it instead would work now and undo itself at the next boot",
			DNSPort, holder)

	case holder.Unit != "":
		return fmt.Errorf(preamble+
			"It is held by %s.\n"+
			"To hand DNS to olr:\n"+
			"  sudo systemctl disable --now %s\n"+
			"`disable --now` rather than `stop`: a stopped unit returns at the next "+
			"boot, and olr's relay then fails to start when nobody is watching",
			DNSPort, holder, holder.Unit)

	default:
		return fmt.Errorf(preamble+
			"It is held by %s, which is not running under a systemd unit — stop it "+
			"however it was started, or move olr's relay with `olr dns set --listen`",
			DNSPort, holder)
	}
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
