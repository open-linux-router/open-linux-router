package devices

import (
	"context"
	"net/netip"
	"slices"
	"strings"
)

// The device list is the join design.md §4.4 describes: identity, which is
// ours and stored, against presence, which is observed and never stored as
// truth. This file is the only place that join happens, which is what makes
// "an operator's answer beats detection" a property of the system rather than a
// convention every caller has to remember.

// FixedAddressView is this module's read-only window onto whoever owns fixed
// addresses — today `dhcp`, through its reservations.
//
// The fixed address is *not* copied into this module's config, and that is the
// point of §4.1: `dhcp` remains the single owner of the fact, and this module
// reads it through an interface rather than keeping a second copy that could
// disagree. What §11.1 actually asked for is that the *workflow* start from the
// device rather than from a MAC typed into a form, and joining it here gives
// every client — UI, CLI, agent — that view without any of them reimplementing
// the join.
type FixedAddressView interface {
	// FixedAddresses maps canonical MAC to reserved address.
	FixedAddresses(ctx context.Context) (map[string]string, error)
}

// NetworkView is this module's read-only window onto whoever owns the address
// ranges — today `dhcp`, through its pools. The same one-way arrow as
// FixedAddressView: this module declares what it needs and never imports dhcp.
//
// It exists because the neighbour table is the only source that reports an
// interface, and it only knows devices it has heard from lately. A device that
// is merely asleep still holds a lease, and placing it by which range its
// address falls in is the difference between a map of the household and a map
// of whatever happens to be awake.
type NetworkView interface {
	// Networks returns the ranges served on each interface.
	Networks(ctx context.Context) ([]Network, error)
}

// Network is one interface and the range served on it.
type Network struct {
	Interface  string
	Start, End netip.Addr
}

// Holds reports whether an address falls inside this network's range.
func (n Network) Holds(addr netip.Addr) bool {
	if !addr.IsValid() || !n.Start.IsValid() || !n.End.IsValid() {
		return false
	}
	// Compare orders v4 before v6, so without this a v4 address would test as
	// being inside every v6 range — an answer that is wrong rather than absent.
	if addr.Is4() != n.Start.Is4() {
		return false
	}
	return addr.Compare(n.Start) >= 0 && addr.Compare(n.End) <= 0
}

// place returns the first network holding the address.
func place(addr netip.Addr, networks []Network) (string, bool) {
	for _, n := range networks {
		if n.Holds(addr) {
			return n.Interface, true
		}
	}
	return "", false
}

// Origin says where a resolved value came from, so a UI can distinguish what it
// was told from what it inferred. A guess presented as a fact is the failure
// mode icon-style-spec.md is written to avoid.
type Origin string

const (
	// OriginOperator is stored intent: a human said so.
	OriginOperator Origin = "operator"

	// OriginDetected is inference from a MAC or a hostname.
	OriginDetected Origin = "detected"

	// OriginObserved is a fact read off the network, such as the hostname a
	// client announced.
	OriginObserved Origin = "observed"

	// OriginNone means nothing could supply a value.
	OriginNone Origin = ""
)

// Resolved is one device with both halves joined.
type Resolved struct {
	MAC string

	// Name and NameOrigin: a stored name, else the hostname the client
	// announced, else empty — in which case a UI falls back to the MAC. An
	// observed hostname is *displayed* but never promoted into stored intent,
	// so the operator's own naming stays distinguishable from the client's.
	Name       string
	NameOrigin Origin

	// Category and CategoryOrigin, resolved in the spec's order: operator, then
	// detection, then nothing.
	Category       Category
	CategoryOrigin Origin

	// Detected is what inference produced regardless of whether it won. Kept so
	// the UI can offer "we think this is a printer — accept?" on a device the
	// operator has never touched, and so an override is visibly an override.
	Detected Detected

	Model string
	Notes string

	// Stored reports whether there is a config entry for this device. False
	// means it is on the list purely because it was seen.
	Stored bool

	// Presence is nil for a device that is stored but has not been seen — the
	// statically-addressed printer between reboots. Nil is meaningfully
	// different from "seen but inactive" and the UI says so differently.
	Presence *Presence

	// FixedIP is the reserved address, owned by dhcp, empty if none.
	FixedIP string

	// Network is which of this router's networks the device is on, and
	// NetworkOrigin says how we know: OriginObserved when a source saw it there,
	// OriginDetected when it was placed by which range its address falls in.
	//
	// The distinction is the same one Name and Category make, and it matters for
	// the same reason. Observed is a fact from the kernel; detected is an
	// inference that a hand-set address outside every pool will quietly fail, so
	// a map drawn from this must be able to say "we could not place it" rather
	// than filing it under a plausible network.
	Network       string
	NetworkOrigin Origin
}

// Online reports whether any source considers the device current.
func (r Resolved) Online() bool {
	return r.Presence != nil && r.Presence.Active
}

// DisplayName is what to show, falling back to the MAC so a device is never
// nameless on screen.
func (r Resolved) DisplayName() string {
	if r.Name != "" {
		return r.Name
	}
	return r.MAC
}

// Build joins stored identity, observed sightings and fixed addresses into the
// list, and returns any problems collected along the way.
//
// The union of three key sets, not just the sightings: a device is on the list
// if a human described it, *or* something saw it, *or* it holds a reservation.
// Dropping any of those three would lose a real case — the printer named last
// year that is currently powered off, the guest phone nobody has named, and the
// reservation made for a device that has not connected yet.
func Build(cfg Config, sightings []Sighting, fixed map[string]string, networks []Network) ([]Resolved, []Problem) {
	presence, problems := Merge(sightings)

	macs := map[string]bool{}
	for _, d := range cfg.Devices {
		macs[d.MAC] = true
	}
	for mac := range presence {
		macs[mac] = true
	}
	for mac := range fixed {
		macs[mac] = true
	}

	out := make([]Resolved, 0, len(macs))
	for mac := range macs {
		out = append(out, resolve(mac, cfg, presence, fixed, networks))
	}

	// Sorted by the name the operator reads, not by presence. Sorting online
	// devices first would reorder the whole list every time a phone slept,
	// which is the row-jumping that makes a table impossible to click in.
	slices.SortStableFunc(out, func(a, b Resolved) int {
		an, bn := strings.ToLower(a.DisplayName()), strings.ToLower(b.DisplayName())
		if n := strings.Compare(an, bn); n != 0 {
			return n
		}
		return strings.Compare(a.MAC, b.MAC)
	})

	return out, problems
}

// resolve applies the resolution order for one device.
func resolve(mac string, cfg Config, presence map[string]Presence, fixed map[string]string, networks []Network) Resolved {
	r := Resolved{MAC: mac}

	if p, ok := presence[mac]; ok {
		copied := p
		r.Presence = &copied
	}

	// Which network the device is on, preferring what a source actually saw.
	// The neighbour table only knows devices it has heard from lately, so the
	// range fallback is what keeps a sleeping phone on the map — and the two are
	// complementary rather than redundant: ARP is the only thing that can place
	// a statically-addressed device, whose address sits outside every pool.
	if r.Presence != nil {
		switch {
		case len(r.Presence.Interfaces) > 0:
			r.Network, r.NetworkOrigin = r.Presence.Interfaces[0], OriginObserved
		default:
			for _, ip := range r.Presence.IPs {
				addr, err := netip.ParseAddr(ip)
				if err != nil {
					continue
				}
				if name, ok := place(addr, networks); ok {
					r.Network, r.NetworkOrigin = name, OriginDetected
					break
				}
			}
		}
	}

	hostname := ""
	if r.Presence != nil {
		hostname = r.Presence.Hostname
	}

	// Detection runs for every device, whether or not it wins, so that the UI
	// can always show what we would have guessed.
	r.Detected = Detect(mac, hostname)

	stored, ok := cfg.Find(mac)
	if ok {
		r.Stored = true
		r.Model = stored.Model
		r.Notes = stored.Notes
	}

	switch {
	case stored.Name != "":
		r.Name, r.NameOrigin = stored.Name, OriginOperator
	case hostname != "":
		r.Name, r.NameOrigin = hostname, OriginObserved
	default:
		r.Name, r.NameOrigin = "", OriginNone
	}

	// The resolution order from icon-style-spec.md, in one place. An operator
	// override is unconditional: a device whose picture was corrected must not
	// have it silently changed back by the next fingerprint update.
	switch {
	case stored.Category != "":
		r.Category, r.CategoryOrigin = stored.Category, OriginOperator
	case r.Detected.Category != "":
		r.Category, r.CategoryOrigin = r.Detected.Category, OriginDetected
	default:
		r.Category, r.CategoryOrigin = CategoryUnknown, OriginNone
	}

	r.FixedIP = fixed[mac]

	return r
}
