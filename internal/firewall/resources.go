package firewall

import "fmt"

// The shared kernel namespaces this module takes a slice of, declared.
//
// design.md §3.4's good-citizen rule is why these are constants in a file of
// their own rather than incidental values scattered through the renderer: the
// whole point is that somebody else can plan around them. A box running olr may
// also be running Docker, firewalld, ufw or a hand-written
// /etc/nftables.conf — every one of which installs NAT chains — and we are not
// alone in that namespace and must not behave as though we are.
//
// This module takes far less than internal/gateway does. It needs no fwmark, no
// RPDB priority and no route table, because a forward is one translation and
// not a decision about where traffic goes. What it takes is one table.
const (
	// TableName is our nftables table. Ours alone — design.md §4.2, "each
	// module writes its own table, never a shared ruleset" — and the name
	// design.md §4 already gives this module.
	//
	// `nft flush ruleset` remains banned outright (§3.4): it silently kills
	// Docker, podman, libvirt and k8s networking. We delete and recreate one
	// table, by name.
	TableName = "olr_nat"

	// PreroutingChain holds the DNAT rules — the translation itself.
	//
	// Hooked at prerouting with the dstnat priority, which is where the
	// destination has to be rewritten: before the routing decision, so the
	// kernel forwards the packet toward the device rather than delivering it
	// locally.
	PreroutingChain = "prerouting"

	// PostroutingChain holds the hairpin masquerade (docs/firewall.md §4).
	//
	// Created only when at least one forward has hairpin on, because a NAT
	// postrouting chain on a box that needs none is a hook registration nobody
	// asked for — and its absence is the honest signal that no source address
	// is being rewritten.
	PostroutingChain = "postrouting"

	// MaxSlot bounds the number of forwards.
	//
	// Two hundred and fifty-six is far past anything a box like this will see,
	// and a bound is what keeps the counter names enumerable and the
	// configuration document a thing somebody can read.
	MaxSlot = 256
)

// Slot identifies one forward's counter across edits.
//
// **Why it is stored rather than derived.** The forwards list is sorted by name
// (Config.Normalize), so a slot derived from position would move every time a
// forward was added or renamed. The number it names is the answer to *has this
// forward ever been hit?* — the first question anybody asks when a port does not
// work — so resetting it because an unrelated row was added is exactly the
// failure named counters exist to prevent (docs/firewall.md §3.4).
//
// This is internal/gateway/resources.go's argument at one quarter the stakes.
// There a wrong slot moves a route table out from under live traffic; here it
// loses a count. The conclusion is the same and the reasoning is worth stating
// once more rather than assumed: allocate once, keep it while the forward
// lives, write it down next to the thing it belongs to.
type Slot int

// Valid reports whether s is inside the documented range.
func (s Slot) Valid() bool { return s >= 1 && s <= MaxSlot }

// Counter is the named nftables counter object for this slot.
//
// Derived from the slot rather than from the operator's name, and for two
// reasons that both have to hold: a name survives being renamed, and `fwd7` is
// certainly a legal nftables identifier while "Minecraft (kids)" is not.
// Legibility is not lost — the rule next to it carries the forward's name in its
// comment, which is what `nft list table inet olr_nat` prints.
func (s Slot) Counter() string { return fmt.Sprintf("fwd%d", int(s)) }

// SlotID returns the forward's slot as the typed value.
func (f Forward) SlotID() Slot { return Slot(f.Slot) }

// Counter is the named counter object this forward's rules increment.
func (f Forward) Counter() string { return f.SlotID().Counter() }

// allocateSlots gives every forward a slot, keeping the ones already assigned.
//
// The rule is "lowest free number", which is only a tie-break — it matters far
// less than what it does *not* do, which is move a forward that already has one.
// Reuse of a freed slot is deliberate and harmless here in a way it is not in
// internal/gateway: what a reused slot inherits is a counter's value, so the
// worst case is a new forward starting with the deleted one's number until the
// next apply rebuilds the table. The alternative — never reusing — runs the
// range out on a box that has been edited enough times.
func (c *Config) allocateSlots() {
	taken := make(map[int]bool, len(c.Forwards))
	for i := range c.Forwards {
		s := c.Forwards[i].Slot
		// A duplicate or out-of-range slot is dropped rather than kept, so a
		// hand-edited file cannot produce two forwards sharing a counter. It is
		// re-allocated below and Validate reports it against a path.
		if !Slot(s).Valid() || taken[s] {
			c.Forwards[i].Slot = 0
			continue
		}
		taken[s] = true
	}

	next := 1
	for i := range c.Forwards {
		if c.Forwards[i].Slot != 0 {
			continue
		}
		for next <= MaxSlot && taken[next] {
			next++
		}
		if next > MaxSlot {
			// Out of slots. Left at zero rather than wrapping, so Validate
			// reports it as the configuration error it is instead of two
			// forwards silently sharing a counter.
			return
		}
		c.Forwards[i].Slot = next
		taken[next] = true
	}
}
