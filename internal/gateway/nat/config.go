// Package nat owns the `olr_nat` table: address translation at the boundary
// between the networks this box serves and everything else, in both
// directions.
//
//   - **Inward** — a **port forward**: something on the internet connects to
//     this router on a port, and the connection is delivered to one device on
//     your network instead. `docs/port-forwarding.md`.
//   - **Outward** — one **egress masquerade** on `dial`'s uplink, so traffic
//     from the networks behind this box has a source address the far side can
//     reply to. `docs/gateway.md` §3.9.
//
// # Why this is a package inside `gateway` rather than a module of its own
//
// It used to be one, called `firewall`, and it never filtered anything — no
// zones, no rule list, no policy. olr is not building filtering for now, so a
// module holding that name and not doing the job was a name blocking the one
// it describes. `docs/port-forwarding.md` §0 has the argument; the short form
// is that translation at a boundary belongs to whoever owns the boundary, and
// that is `gateway`.
//
// It is a *package* rather than a flat merge into `internal/gateway` because
// the two halves share almost eighty identifier names — `Config`, `Desired`,
// `Plan`, `Render`, `table`, `verb`, and so on down — nearly all of them
// parallel boilerplate rather than one concept spelled twice. Merging flat
// would have meant renaming most of them; a package boundary costs one import
// and keeps both halves readable. The seam is deliberately narrow: `gateway`
// owns the stored document and hands this package a Config (see Applier), and
// this package owns the table and everything about how it is rendered.
//
// The consequence worth knowing before reading any of this: **olr does not
// maintain a default-deny stance on forwarded traffic**, so creating a forward
// does not open a hole in a firewall of ours — there is not one. It installs a
// NAT translation. If something else on the box filters the forward hook, that
// filter still applies, and `docs/port-forwarding.md` §5.2 is why we report
// that rather than trying to overrule it.
package nat

import (
	"net/netip"
	"slices"
	"strings"
)

// Module is the path segment and event label these routes are served under.
//
// `gateway`'s, not one of this package's own: the forwards live in `gateway`'s
// config section and are served under its prefix. The constant exists so that
// the endpoints in cli.go and the events in http.go cannot drift from the
// module that actually owns them.
const Module = "gateway"

// Config is what this package needs in order to render the table.
//
// **Not a stored document section.** `gateway` owns the document and projects
// into this — see gateway.Config.NAT — which is why there is no FromDocument
// here and why the JSON tags stop at Forward. Keeping the projection as a
// struct rather than threading three arguments through Validate, BuildPlan and
// Render is what let the whole of that pipeline survive the move from a module
// of its own unchanged.
type Config struct {
	// Enabled controls whether any of this is programmed into the kernel at
	// all. It is `gateway.Config.Enabled` — one switch for the module, which
	// means **turning the module off closes every forwarded port** as well as
	// tearing down policy routing. docs/gateway.md §0 says so in those words,
	// and the plan and the UI repeat it, because a switch that silently shuts
	// a port somebody is reaching a service through is the thing this surface
	// must not do quietly.
	Enabled bool

	// Forwards are the standing instructions. Empty means nothing from outside
	// reaches anything inside, which is what an unconfigured box already does.
	Forwards []Forward

	// Egress is the one masquerade rule, nil when there is nothing to write —
	// no uplink, no networks, or the operator turned it off. docs/gateway.md
	// §3.9 has the whole argument; from this package's side it is simply
	// another rule in the table it owns.
	Egress *Egress
}

// Egress is the outbound half: rewrite the source address of traffic from
// these subnets as it leaves this interface.
//
// One rule, not one per subnet, because nftables takes a set and a set reads
// better in `nft list table inet olr_nat` than five near-identical lines.
type Egress struct {
	// Interface is `dial`'s uplink. Never empty when Egress is non-nil —
	// gateway does not build one without it, because "masquerade out of
	// whichever interface seems outward" is exactly the guess design.md §5.6
	// forbids.
	Interface string

	// Sources are the subnets `link` declares, in canonical order. Never empty
	// when Egress is non-nil: a masquerade with no source set would either
	// match everything or nothing, and both are worse than not writing it.
	Sources []netip.Prefix
}

// Empty reports whether an egress rule would write nothing.
func (e *Egress) Empty() bool {
	return e == nil || e.Interface == "" || len(e.Sources) == 0
}

// Forward is one standing instruction: connections arriving on this interface,
// on this port, go to that address and port instead.
//
// Flat and independent by design. There is no ladder and no inheritance here,
// unlike internal/gateway's assignments, and docs/port-forwarding.md §2 has the
// argument: "where does my phone go?" has an answer whether or not anybody set
// one, while "what reaches my NAS from outside?" is *nothing* until somebody
// writes it down — so the list of what they wrote down is the whole model.
type Forward struct {
	// Name is what the operator calls it — "web", "minecraft", "vpn". It is the
	// identity every surface refers to it by, so renaming is a rename
	// everywhere; see Config.Rename.
	Name string `json:"name"`

	// In is the interface connections arrive on.
	//
	// An interface rather than "the internet", and that is not a placeholder
	// for a better word: a box can have two uplinks, and a forward that
	// silently applied to both would be a policy nobody wrote. It becomes a
	// network name when link grows networks (docs/gateway.md §2.5's staging), and
	// the stored document does not change shape when it does.
	In string `json:"in"`

	// Protocol is tcp, udp or both. Empty means tcp.
	Protocol Protocol `json:"protocol,omitempty"`

	// Port is the port, or ports, connections arrive on.
	Port PortRange `json:"port"`

	// To is where they go instead: an address on this network and the port to
	// deliver to.
	//
	// An address and not a reference to a device in the inventory, and
	// docs/port-forwarding.md §1.2 is the argument. A forward is a kernel rule that
	// has to exist at boot, before any lease has been handed out and while the
	// device is switched off, so a device reference would either leave the port
	// dead until the device appeared or be resolved against a remembered
	// address — which is an address with one more place to disagree. The UI
	// offers a device picker that fills this in and warns when that device has
	// no fixed address; what is stored is the address either way.
	//
	// When Port is a range, this names the first port of the same-width range
	// inside, and Validate insists it *is* the same range — see PortRange and
	// docs/port-forwarding.md §1.3.
	To netip.AddrPort `json:"to"`

	// Hairpin makes the forward work from inside the network too (§4). Nil
	// means on.
	//
	// A pointer because the default is *on* and a plain bool could not tell
	// "the operator turned it off" from "the field was never written" — the
	// distinction that decides whether a stored document from an older olr
	// keeps behaving the way it did.
	//
	// On by default because off-by-default is a setting nobody discovers: what
	// it prevents does not present as a missing feature, it presents as the
	// port forward being broken, tested from the one machine the operator has
	// to hand. What it costs is that the device sees the router's address
	// rather than the real client's, which Validate warns about every time.
	Hairpin *bool `json:"hairpin,omitempty"`

	// Slot is the only field an operator never sets: it is allocated on save
	// and preserved thereafter.
	//
	// It exists for one thing — the named counter (docs/port-forwarding.md §3.4) —
	// and it is stored rather than derived for internal/gateway/resources.go's
	// reason, one size down. Deriving it from position in a sorted list means
	// adding a forward called "backup" renumbers "web", and the number an
	// operator is watching to answer *has this ever been hit?* resets because
	// of an unrelated row. It is also the only spelling of a counter name that
	// is certainly a legal nftables identifier, which an operator-chosen name
	// is not.
	Slot int `json:"slot"`
}

// MaxNameLen bounds a forward's name so a UI can lay out a row without
// defending against a megabyte in a field.
const MaxNameLen = 64

// HairpinOrDefault resolves the nil pointer: on.
func (f Forward) HairpinOrDefault() bool {
	if f.Hairpin == nil {
		return true
	}
	return *f.Hairpin
}

// ProtocolOrDefault resolves the empty protocol.
//
// tcp, because it is the overwhelming majority of what anybody forwards and
// because the alternative — refusing a forward that did not say — would make
// the common command longer to serve the rare one. `olr gateway show forwards`
// prints the column, so a forward that should have been udp is visible rather
// than mysterious.
func (f Forward) ProtocolOrDefault() Protocol {
	if f.Protocol == "" {
		return ProtocolTCP
	}
	return f.Protocol
}

// Find returns the forward with this name, or false.
func (c Config) Find(name string) (Forward, bool) {
	for _, f := range c.Forwards {
		if f.Name == name {
			return f, true
		}
	}
	return Forward{}, false
}

// Empty reports whether anything has been configured at all.
func (c Config) Empty() bool { return len(c.Forwards) == 0 }

// Normalize puts the config in canonical form: names and interfaces trimmed,
// forwards sorted, slots allocated.
//
// Sorting is not cosmetic. The list is rendered in stored order, so an
// append-on-edit would make a row jump to the bottom the moment it was renamed
// — the bug internal/devices called out in the dhcp tables. Sorting on the way
// in means position is a function of identity, not of edit history.
//
// It deliberately does not validate: a malformed forward is left as-is for
// Validate to report against a proper path.
func (c *Config) Normalize() {
	for i := range c.Forwards {
		c.Forwards[i].Name = strings.TrimSpace(c.Forwards[i].Name)
		c.Forwards[i].In = strings.TrimSpace(c.Forwards[i].In)
		c.Forwards[i].Protocol = Protocol(strings.TrimSpace(string(c.Forwards[i].Protocol)))
	}

	slices.SortStableFunc(c.Forwards, func(a, b Forward) int {
		return strings.Compare(a.Name, b.Name)
	})

	// Last, because it depends on the names being settled and it is the one
	// step here that invents a value rather than tidying one.
	c.allocateSlots()
}

// Upsert adds a forward or replaces the one with the same name, keeping its
// slot.
//
// Keeping the slot is the point of the method existing at all: an edit that
// silently reallocated it would reset the counter somebody is watching to
// decide whether the forward is being hit.
func (c *Config) Upsert(f Forward) {
	for i := range c.Forwards {
		if c.Forwards[i].Name == f.Name {
			if f.Slot == 0 {
				f.Slot = c.Forwards[i].Slot
			}
			c.Forwards[i] = f
			c.Normalize()
			return
		}
	}
	c.Forwards = append(c.Forwards, f)
	c.Normalize()
}

// Remove drops a forward, reporting whether it was there.
func (c *Config) Remove(name string) bool {
	for i, f := range c.Forwards {
		if f.Name == name {
			c.Forwards = append(c.Forwards[:i], c.Forwards[i+1:]...)
			return true
		}
	}
	return false
}

// Rename changes a forward's name, keeping its slot and so its counter.
//
// Nothing else in this module references a forward by name — there is no
// Default and no assignment list, which is the one way this object is simpler
// than internal/gateway's exit. So this is a rename in one place, and it exists
// as a method rather than as two edits only so that the slot survives it.
func (c *Config) Rename(from, to string) bool {
	f, ok := c.Find(from)
	if !ok {
		return false
	}
	f.Name = to
	c.Remove(from)
	c.Forwards = append(c.Forwards, f)
	c.Normalize()
	return true
}
