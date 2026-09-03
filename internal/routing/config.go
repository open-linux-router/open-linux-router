// Package routing decides where a packet goes.
//
// The object it owns is an **exit**: anything that accepts traffic addressed
// somewhere else and takes responsibility for delivering it (docs/gateway.md
// §1.1). That is a membership test rather than a label, and it is what settles
// the question the naming kept failing at — a WireGuard interface, a proxy box
// on the LAN and `unreachable` are all exits, while a SOCKS5 port is not,
// because the client has to *ask* in its own protocol.
//
// The operator never meets the word. They meet one sentence with a preposition
// doing the work — *Internet via [ Clash ]* — attached to the thing they are
// looking at, inherited most-specific-wins (§2). An ordered first-match rule
// list is what the kernel gets; it is deliberately not what the screen shows,
// because answering "where does my phone actually go?" against one requires
// simulating evaluation.
package routing

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// ModuleName is the path segment, config section and event label for this
// module.
const ModuleName = "routing"

// Config is the routing module's intent — what the operator asked for, not what
// is running. It is the single source for the CLI flags, the REST body, the UI
// form, and the MCP tool definition (design.md §3.2 rule 3), so a field added
// here appears on every surface without further work.
//
// Fields without `omitempty` are reflected as schema-required (design.md §10,
// config format), so the tags are load-bearing.
type Config struct {
	// Enabled controls whether any of this is programmed into the kernel at
	// all. Disabling tears down our table, our rules and our route tables and
	// leaves the box routing exactly as it did before olr was installed —
	// which is also what makes the module safe to try (design.md §7).
	//
	// Keeping the configuration while off means it can be turned back on
	// without retyping it, matching internal/dhcp and internal/dns.
	Enabled bool `json:"enabled"`

	// Exits are the ways out. Empty means everything uses the box's own default
	// route, which is what an unconfigured box already does.
	Exits []Exit `json:"exits,omitempty"`

	// Default names the exit that everything uses unless something more
	// specific says otherwise — the top rung of §2.1's ladder.
	//
	// Empty means the box's own default route, and that is deliberately not
	// spelled as a reserved exit name. §1.2 says the box's default gateway *is*
	// an exit, but it is one we neither configure nor own: it is whatever is in
	// the main table, put there by `dial` or by DHCP or by hand. Modelling it
	// as a row the operator could edit would promise a control we do not have.
	Default string `json:"default,omitempty"`

	// Interfaces assigns an exit per network — §2.5's first tier of the ladder,
	// and the only one that ships here.
	//
	// Keyed by kernel interface name rather than by a group, matching
	// internal/dhcp's pools and for the reason internal/dns states in as many
	// words: groups do not exist until the link module lands (design.md
	// milestone 1), and inventing a second spelling now would be one more thing
	// to unpick then. The name of this field changes when link does; the shape
	// does not.
	Interfaces []Assignment `json:"interfaces,omitempty"`
}

// Assignment is one rung of the ladder: this source uses that exit.
//
// §2.1's tag and device tiers are v2 (§9), waiting on design.md §10 decision 6.
// They refine this same field rather than replacing it, which is the whole
// argument for shipping the ladder one rung at a time — *most specific wins*
// was true from the first version, so the mental model does not change when the
// other rungs arrive.
type Assignment struct {
	// Interface is the source network, named by its kernel interface.
	Interface string `json:"interface"`

	// Exit names an exit, or is empty to inherit Config.Default.
	//
	// Empty is a real value and not a missing one: *IoT devices go direct* is
	// the case §2.1 opens with, and the answer is that the IoT network keeps
	// the default — nothing to configure, because it is already right.
	Exit string `json:"exit,omitempty"`
}

// Exit is one way out of the box.
type Exit struct {
	// Name is what the operator calls it — "Clash", "Modem", "Blocked". It is
	// also the reference used by Default and by every Assignment, so renaming
	// one is a rename everywhere; see Config.Rename.
	Name string `json:"name"`

	// Via is how traffic actually leaves. §1.2's forms, minus TPROXY.
	Via Via `json:"via"`

	// Slot is the only field an operator never sets: it is allocated on save
	// and preserved thereafter. It is written down rather than derived because
	// every kernel resource this module owns is computed from it — see
	// resources.go, which is also where the argument for storing it lives.
	Slot int `json:"slot"`

	// IPv6 is the exit's answer to §5.4, and there is no useful default that
	// works for every form, so Resolve fills it in per form rather than a
	// constant doing it.
	//
	// The failure it exists to prevent is the quietest one in the document: if
	// the exit carries v4 only and clients have working IPv6, everything with
	// an AAAA record goes out the default path at full speed and nothing looks
	// wrong. That is the difference between working and appearing to work.
	IPv6 IPv6Mode `json:"ipv6,omitempty"`

	// OnFailure is what happens to assigned traffic when the probe says this
	// exit is down. Empty means FailBlock.
	OnFailure FailureMode `json:"on_failure,omitempty"`

	// SNAT rewrites the source address of traffic sent to a next-hop exit, so
	// the far box replies to us rather than straight to the client over the
	// shared L2 (§5.3). Nil means on for ViaNextHop and is ignored otherwise.
	//
	// A pointer because the default is *on* and a plain bool could not tell
	// "the operator turned it off" from "the field was never written" — which
	// is exactly the distinction that decides whether a stored document from an
	// older olr keeps behaving the way it did.
	//
	// What it costs is real and worth stating on the screen: the proxy box
	// stops seeing which client a connection came from, so its own per-source
	// rules stop working. That is acceptable here only because source selection
	// is a thing we took over. An operator who wants the far box to keep doing
	// its own per-source work turns this off and gives that box a network of
	// its own — §11 open decision 2's other answer.
	SNAT *bool `json:"snat,omitempty"`

	// Probe is the through-path health check (§5.5). Nil means the exit is
	// never probed and so is never considered down.
	//
	// Not probing is a legitimate choice for `blocked` and for an exit whose
	// liveness is already visible some other way, but it is the wrong default
	// for a next hop: docs/dns.md §1.2 rests the whole "DHCP hands out olr, and
	// only olr" topology rule on olr being able to re-point a dead exit,
	// because IPv4 has no gateway failover at the device layer. Validate warns
	// when a routing exit has no probe rather than silently accepting it.
	Probe *Probe `json:"probe,omitempty"`
}

// Via is how an exit delivers traffic. §1.2's forms.
//
// The doc writes these as four shapes discriminated by which key is present.
// This carries an explicit Kind instead, for three reasons that all point the
// same way: the discriminator publishes through core.Reflect as an enum a UI
// form can switch on, "exactly the fields for this kind" becomes one flat
// validation rule instead of a count of set fields, and a document that names
// its own shape survives a field being added later without the meaning of the
// old documents changing.
type Via struct {
	// Kind selects the form. See ViaKinds for the vocabulary.
	Kind ViaKind `json:"kind"`

	// Interface names the device traffic leaves through: a WireGuard or
	// Tailscale interface, a PPPoE session, a proxy's TUN. Only for
	// ViaInterface.
	//
	// There is deliberately no separate `tun` or `vpn` form (§1.2). A TUN
	// device is an instance of this, and giving it its own kind would force the
	// same question for every other interface-shaped thing until the schema had
	// a kind per vendor.
	Interface string `json:"interface,omitempty"`

	// NextHop is the address of a box that will take the traffic on — the
	// modem, or a machine on the LAN running mihomo. Only for ViaNextHop.
	NextHop *netip.Addr `json:"next_hop,omitempty"`

	// Dev is the interface the next hop is reachable through. Only for
	// ViaNextHop, and optional: empty means the kernel resolves it from the
	// main table, which is right whenever the next hop is on a network we
	// already have an address on. Validate checks that it is.
	Dev string `json:"dev,omitempty"`
}

// ViaKind names one of §1.2's forms.
type ViaKind string

const (
	// ViaInterface sends traffic out a device. wg0, tailscale0, ppp0, a proxy's
	// TUN — all the same thing to us.
	ViaInterface ViaKind = "interface"

	// ViaNextHop hands traffic to another box, which is the form the reference
	// topology in docs/dns.md §1 is built from: the modem is a next hop and so
	// is the proxy box, and neither is something a device is ever told about.
	ViaNextHop ViaKind = "next_hop"

	// ViaBlocked refuses traffic, explicitly. It is an exit by §1.1's test
	// because refusing *is* taking responsibility, and it is the answer to one
	// of the two most common requests that turn out not to involve a proxy at
	// all — "IoT gets no internet" (§2.4).
	ViaBlocked ViaKind = "blocked"

	// ViaLocalSocket — TPROXY — is v2 (§9) and deliberately absent from the
	// vocabulary rather than declared and refused. An enum value that every
	// generated surface offers and validation then rejects is a lie in the
	// schema, and the schema is the contract three other surfaces are built
	// from.
)

// ViaKinds lists the vocabulary, for flag help and the schema enum.
func ViaKinds() []ViaKind { return []ViaKind{ViaInterface, ViaNextHop, ViaBlocked} }

// Valid reports whether k is a known form. The empty string is not: unlike the
// enums that have a sensible default, there is no form to fall back to, and
// guessing one would silently route traffic somewhere nobody asked for.
func (k ViaKind) Valid() bool { return slices.Contains(ViaKinds(), k) }

// Routing reports whether this form leaves through a route table.
//
// ViaInterface and ViaNextHop are both routing and differ by one field;
// ViaBlocked is routing too, with an unreachable route rather than a real one.
// The split that matters is against ViaLocalSocket, where the packet is never
// routed at all — and that form does not exist yet, which is why this reads as
// a tautology today and will not once TPROXY lands.
func (k ViaKind) Routing() bool { return k == ViaInterface || k == ViaNextHop || k == ViaBlocked }

// IPv6Mode is an exit's answer to §5.4.
type IPv6Mode string

const (
	// IPv6Via carries IPv6 through the exit alongside IPv4. Correct when the
	// exit actually has IPv6 — a WireGuard tunnel with a v6 address, a next hop
	// that routes v6.
	IPv6Via IPv6Mode = "via"

	// IPv6Block sends IPv6 from assigned sources to `unreachable` instead of
	// letting it take the default path. This is the right answer for a v4-only
	// exit, and it fails loudly: a browser tries AAAA, gets an immediate
	// refusal, and falls straight back to IPv4.
	IPv6Block IPv6Mode = "block"

	// IPv6Direct lets IPv6 from assigned sources take the box's default path.
	// Available, never a default: it is the exact leak §5.4 is about, and the
	// operator has to have typed it.
	IPv6Direct IPv6Mode = "direct"
)

// IPv6Modes lists the vocabulary.
func IPv6Modes() []IPv6Mode { return []IPv6Mode{IPv6Via, IPv6Block, IPv6Direct} }

// Valid reports whether m is known. Empty is valid and resolves per form.
func (m IPv6Mode) Valid() bool { return m == "" || slices.Contains(IPv6Modes(), m) }

// FailureMode is what happens to assigned traffic when the exit's probe fails.
type FailureMode string

const (
	// FailBlock stops the traffic, which is the default. The UI can then say
	// "Living Room TV: no internet — Clash is down", and that is a sentence
	// somebody can act on.
	FailBlock FailureMode = "block"

	// FailDirect sends the traffic out the box's default path instead. It
	// silently leaks exactly what the operator asked to route, so it is
	// available and never the default.
	FailDirect FailureMode = "direct"
)

// FailureModes lists the vocabulary.
func FailureModes() []FailureMode { return []FailureMode{FailBlock, FailDirect} }

// Valid reports whether f is known. Empty is valid and means FailBlock.
func (f FailureMode) Valid() bool { return f == "" || slices.Contains(FailureModes(), f) }

// OrDefault resolves the empty string.
func (f FailureMode) OrDefault() FailureMode {
	if f == "" {
		return FailBlock
	}
	return f
}

// Probe is a through-path health check for one exit (§5.5).
//
// Through-path, not a ping, and the distinction is the whole point: a crashed
// mihomo on a live Debian box answers ARP and ICMP indefinitely while
// forwarding nothing — and worse, loops our traffic back at us, because its own
// default gateway is us. Only an end-to-end connection through the exit can
// tell that apart from a healthy one.
type Probe struct {
	// Target is what we connect to, as address:port. An address rather than a
	// name on purpose: resolving it would make the probe depend on DNS, so a
	// resolver problem would present as every exit being down at once.
	Target netip.AddrPort `json:"target"`

	// Interval is how often to check. Zero means DefaultProbeInterval.
	Interval Duration `json:"interval,omitempty"`

	// Timeout bounds one attempt. Zero means DefaultProbeTimeout. It must be
	// shorter than Interval or attempts overlap.
	Timeout Duration `json:"timeout,omitempty"`

	// Failures is how many consecutive failures mark the exit down, and
	// Successes how many consecutive successes bring it back. Zero means the
	// defaults below.
	//
	// Hysteresis in both directions, per §5.5, and it is not symmetric by
	// accident: coming back should be slower than going down is, because a
	// half-recovered proxy that flaps is worse for the house than one that
	// stays down until somebody looks at it.
	Failures  int `json:"failures,omitempty"`
	Successes int `json:"successes,omitempty"`
}

// Probe defaults. Thirty seconds is slow enough to be invisible on the wire and
// fast enough that a dead exit is caught before anyone files a complaint.
const (
	DefaultProbeInterval  = Duration(30 * time.Second)
	DefaultProbeTimeout   = Duration(5 * time.Second)
	DefaultProbeFailures  = 3
	DefaultProbeSuccesses = 2
)

// MinProbeInterval is a floor, so that a typo cannot turn the probe into a
// connection flood against somebody else's server.
const MinProbeInterval = Duration(time.Second)

// Resolved fills in every zero value, so the prober never has to.
func (p Probe) Resolved() Probe {
	if p.Interval <= 0 {
		p.Interval = DefaultProbeInterval
	}
	if p.Timeout <= 0 {
		p.Timeout = DefaultProbeTimeout
	}
	if p.Failures <= 0 {
		p.Failures = DefaultProbeFailures
	}
	if p.Successes <= 0 {
		p.Successes = DefaultProbeSuccesses
	}
	return p
}

// MaxNameLen bounds an exit name so a UI can lay out a row without defending
// against a megabyte in a field. Generous enough that no real name hits it.
const MaxNameLen = 64

// SNATOrDefault resolves the nil pointer: on for a next hop, off for everything
// else, per §5.3's recommendation for v1.
func (e Exit) SNATOrDefault() bool {
	if e.Via.Kind != ViaNextHop {
		return false
	}
	if e.SNAT == nil {
		return true
	}
	return *e.SNAT
}

// IPv6OrDefault resolves the empty IPv6 mode per form.
//
// The default is IPv6Block for every form, and that is the conservative answer
// rather than the convenient one. An exit whose v6 capability we cannot see
// from here is one where "carry it" might work and might leak; blocking fails
// immediately and visibly, which is the failure an operator can diagnose. An
// exit that really does carry v6 says so in one field.
func (e Exit) IPv6OrDefault() IPv6Mode {
	if e.IPv6 == "" {
		return IPv6Block
	}
	return e.IPv6
}

// Find returns the exit with this name, or false.
func (c Config) Find(name string) (Exit, bool) {
	for _, e := range c.Exits {
		if e.Name == name {
			return e, true
		}
	}
	return Exit{}, false
}

// Assigned reports the exit name in force for an interface, and where it came
// from — §2.2's *effective value, with its source*.
//
// Inheritance is unusable if the answer is not visible, so this is the function
// every surface asks rather than each one reimplementing the ladder. It returns
// the empty exit name for "the box's own default route", which is a real answer
// and not an absent one.
func (c Config) Assigned(iface string) (exit string, source Source) {
	for _, a := range c.Interfaces {
		if a.Interface != iface {
			continue
		}
		if a.Exit == "" {
			// Explicitly set to inherit. Reported as coming from the default
			// rather than from the network, because that is what an operator
			// changing the default needs to know: this row will follow.
			return c.Default, SourceDefault
		}
		return a.Exit, SourceInterface
	}
	return c.Default, SourceDefault
}

// Source says which rung of §2.1's ladder produced an effective value.
type Source string

const (
	// SourceDefault is the box-wide setting.
	SourceDefault Source = "default"
	// SourceInterface is a per-network assignment.
	SourceInterface Source = "interface"
)

// InUse reports whether any assignment, or the default, names this exit.
//
// Used by the impact classifier: changing an exit nobody routes through cannot
// disturb anybody, and saying so is what keeps `disruptive` from crying wolf.
func (c Config) InUse(name string) bool {
	if name == "" {
		return false
	}
	if c.Default == name {
		return true
	}
	for _, a := range c.Interfaces {
		if a.Exit == name {
			return true
		}
	}
	return false
}

// UsedBy lists the interfaces whose traffic goes through this exit, sorted.
func (c Config) UsedBy(name string) []string {
	if name == "" {
		return nil
	}
	var out []string
	for _, a := range c.Interfaces {
		if got, _ := c.Assigned(a.Interface); got == name {
			out = append(out, a.Interface)
		}
	}
	sort.Strings(out)
	return out
}

// UnmarshalConfig parses a document strictly.
//
// Unknown fields are rejected for the reason internal/dhcp gives on the PUT
// path: a mistyped key that silently did nothing would be the worst outcome — a
// 200, an operator who believes the setting took, and a screen that disagrees.
func UnmarshalConfig(data []byte) (Config, error) {
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()

	var c Config
	if err := dec.Decode(&c); err != nil {
		return Config{}, fmt.Errorf("parsing routing config: %w", err)
	}
	c.Normalize()
	return c, nil
}

// MarshalConfig renders intent for the store.
func MarshalConfig(c Config) ([]byte, error) {
	c.Normalize()
	return json.MarshalIndent(c, "", "  ")
}

// FromDocument reads this module's subtree out of the configuration document.
//
// A document without a "routing" key is not an error — it means nobody has
// configured an exit, which is exactly what a fresh install looks like and what
// the screen must render as an empty state rather than a failure.
func FromDocument(d core.Document) (Config, error) {
	raw, ok := d.Raw(ModuleName)
	if !ok {
		return Config{}, nil
	}
	c, err := UnmarshalConfig(raw)
	if err != nil {
		return Config{}, fmt.Errorf("%s configuration: %w", ModuleName, err)
	}
	return c, nil
}

// Normalize puts the config in canonical form: names and interfaces trimmed,
// exits and assignments sorted, slots allocated.
//
// Sorting is not cosmetic. The lists are rendered in stored order, so an
// append-on-edit would make a row jump to the bottom the moment it was renamed
// — the bug internal/devices called out in the dhcp tables. Sorting on the way
// in means position is a function of identity, not of edit history.
//
// It deliberately does not validate: a malformed exit is left as-is for
// Validate to report against a proper path.
func (c *Config) Normalize() {
	for i := range c.Exits {
		c.Exits[i].Name = strings.TrimSpace(c.Exits[i].Name)
		c.Exits[i].Via.Interface = strings.TrimSpace(c.Exits[i].Via.Interface)
		c.Exits[i].Via.Dev = strings.TrimSpace(c.Exits[i].Via.Dev)
	}
	c.Default = strings.TrimSpace(c.Default)
	for i := range c.Interfaces {
		c.Interfaces[i].Interface = strings.TrimSpace(c.Interfaces[i].Interface)
		c.Interfaces[i].Exit = strings.TrimSpace(c.Interfaces[i].Exit)
	}

	slices.SortStableFunc(c.Exits, func(a, b Exit) int { return strings.Compare(a.Name, b.Name) })
	slices.SortStableFunc(c.Interfaces, func(a, b Assignment) int {
		return strings.Compare(a.Interface, b.Interface)
	})

	// Last, because it depends on the names being settled and it is the one
	// step here that invents a value rather than tidying one.
	c.allocateSlots()
}

// Upsert adds an exit or replaces the one with the same name, keeping its slot.
//
// Keeping the slot is the point of the method existing at all: an edit that
// silently reallocated it would move the exit's route table out from under
// every flow currently marked for it.
func (c *Config) Upsert(e Exit) {
	for i := range c.Exits {
		if c.Exits[i].Name == e.Name {
			if e.Slot == 0 {
				e.Slot = c.Exits[i].Slot
			}
			c.Exits[i] = e
			c.Normalize()
			return
		}
	}
	c.Exits = append(c.Exits, e)
	c.Normalize()
}

// SetAssignment points one network at an exit, replacing any it already had.
//
// An empty exit name is meaningful and is kept rather than dropped: it records
// "this network explicitly follows the box-wide setting", which reads
// differently on screen from a network nobody has thought about. Use
// RemoveAssignment to say the second thing.
func (c *Config) SetAssignment(iface, exit string) {
	for i := range c.Interfaces {
		if c.Interfaces[i].Interface == iface {
			c.Interfaces[i].Exit = exit
			c.Normalize()
			return
		}
	}
	c.Interfaces = append(c.Interfaces, Assignment{Interface: iface, Exit: exit})
	c.Normalize()
}

// RemoveAssignment drops a network's override, reporting whether it had one.
func (c *Config) RemoveAssignment(iface string) bool {
	for i, a := range c.Interfaces {
		if a.Interface == iface {
			c.Interfaces = append(c.Interfaces[:i], c.Interfaces[i+1:]...)
			return true
		}
	}
	return false
}

// Remove drops an exit, reporting whether it was there.
//
// It deliberately does not clean up references to the name. A dangling Default
// or Assignment is a validation error with a message naming both ends, which is
// a better outcome than silently re-pointing somebody's phones at the modem
// because an exit was deleted in another tab.
func (c *Config) Remove(name string) bool {
	for i, e := range c.Exits {
		if e.Name == name {
			c.Exits = append(c.Exits[:i], c.Exits[i+1:]...)
			return true
		}
	}
	return false
}

// Rename changes an exit's name and every reference to it, together.
//
// Together is the whole reason this is a method rather than two edits: an exit
// is referenced by name from Default and from every Assignment, so renaming in
// one place and not the others is how a rename turns into an outage.
func (c *Config) Rename(from, to string) bool {
	e, ok := c.Find(from)
	if !ok {
		return false
	}
	e.Name = to
	c.Remove(from)
	c.Exits = append(c.Exits, e)
	if c.Default == from {
		c.Default = to
	}
	for i := range c.Interfaces {
		if c.Interfaces[i].Exit == from {
			c.Interfaces[i].Exit = to
		}
	}
	c.Normalize()
	return true
}

// Empty reports whether anything has been configured at all.
func (c Config) Empty() bool {
	return len(c.Exits) == 0 && len(c.Interfaces) == 0 && c.Default == ""
}
