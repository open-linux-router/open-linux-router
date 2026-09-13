package dns

import (
	"fmt"
	"net/netip"
)

// WithDerivedListen fills in where DNS answers, when the operator turned it on
// and did not say.
//
// This is the fix for the worst moment a new box had. `enabled: true` with an
// empty `listen` is refused by Validate — correctly, because a relay that
// answers nowhere is a DNS outage that looks like a working configuration — and
// the refusal arrived in the UI as a red toast naming a field the operator had
// never seen, on the very first switch they flipped. The router knew the answer
// the whole time: it is its own address on the networks it was given.
//
// Derived once, at the moment of the write, and stored in intent. Not resolved
// at runtime, which is the tempting version and is wrong twice over:
//
//   - design.md §5.4 needs drift to stay decidable. "Plan the stored intent
//     against reality" cannot answer anything if the intent means "whatever the
//     interfaces happen to be today".
//   - Adopting an interface later would silently extend the resolver onto it.
//     internal/link has no notion of which interface faces the internet, so the
//     first time that bit somebody it would be an open resolver on their WAN —
//     the amplifier docs/dns.md §5 exists to prevent.
//
// Writing it once has neither problem: the config says 192.168.1.91:53, and it
// goes on saying so no matter what is adopted afterwards. design.md §5.6 calls
// this being smart at setup rather than at runtime.
//
// Returns the config unchanged and no notes whenever there is nothing to do —
// DNS off, an address already set, or no adopted interface to take one from.
// The last case stays a validation error, because "nothing was given to this
// router" is a thing to say, not a thing to guess at.
func (c Config) WithDerivedListen(links LinkView) (Config, []string) {
	if !c.Enabled || len(c.Listen) > 0 {
		return c, nil
	}

	infos, err := links.Interfaces()
	if err != nil {
		// The error is not swallowed, it is deferred: every rule that needs
		// this view already reports an unreadable one, and failing here would
		// turn "we could not read the interfaces" into a message about a
		// listen address.
		return c, nil
	}

	var (
		listen []netip.AddrPort
		notes  []string
		seen   = map[netip.AddrPort]bool{}
	)
	for _, info := range infos {
		if !info.Adopted {
			// Adoption is the permission (design.md §3.4/§7). An address on an
			// interface nobody handed us is exactly the address we must not
			// start answering on by ourselves.
			continue
		}
		// Adoption alone is not enough to *choose* an address, only to accept
		// one. The WAN gets adopted too — gateway and firewall need it — and a
		// derivation that took every adopted address would put a resolver on
		// the public side of a box whose operator asked for DNS on their LAN.
		// That is the amplifier docs/dns.md §5 refuses, arrived at by helpful
		// default, which is the worst way to arrive at it. So the rule is
		// narrower than adoption: private addresses only. Anything else is a
		// deliberate enough arrangement that the operator can name it.
		// IPv4 first, then IPv6, so a dual-stack interface reads in the order
		// an operator will check them in.
		for _, wantV4 := range []bool{true, false} {
			for _, prefix := range info.Prefixes {
				addr := prefix.Addr()
				if addr.Is4() != wantV4 || !usableListen(addr) {
					continue
				}
				at := netip.AddrPortFrom(addr, DNSPort)
				if seen[at] {
					continue
				}
				seen[at] = true
				listen = append(listen, at)
				notes = append(notes, fmt.Sprintf("answering DNS on %s (%s)", at, info.Name))
			}
		}
	}
	if len(listen) == 0 {
		return c, nil
	}

	c.Listen = listen
	return c, notes
}

// anyAdopted reports whether the operator has handed this router anything at
// all. It separates "you have not set a listen address" from "there is nothing
// here that could be one", which are different problems with different fixes.
func anyAdopted(links LinkView) bool {
	infos, err := links.Interfaces()
	if err != nil {
		// Unreadable is not the same as empty, and guessing "empty" here would
		// tell an operator with a working box to go adopt an interface they
		// already adopted.
		return true
	}
	for _, info := range infos {
		if info.Adopted {
			return true
		}
	}
	return false
}

// usableListen reports whether an address is one this module may choose by
// itself — a home network's own address, and nothing else.
//
// IsPrivate is RFC 1918 and RFC 4193, so it reads both halves of a dual-stack
// LAN and neither half of an uplink. Link-local is excluded separately and is
// worth naming: every IPv6 interface has an fe80:: address, it is not routable
// off the segment, and binding it would produce a listen address that looks
// configured and answers almost nobody.
func usableListen(addr netip.Addr) bool {
	switch {
	case !addr.IsValid(), addr.IsUnspecified(), addr.IsLoopback():
		return false
	case addr.IsLinkLocalUnicast(), addr.IsLinkLocalMulticast(), addr.IsMulticast():
		return false
	}
	return addr.IsPrivate()
}
