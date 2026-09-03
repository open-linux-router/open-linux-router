package routing

// The shared kernel namespaces this module takes a slice of, declared
// (docs/gateway.md §3.2).
//
// design.md §3.4's good-citizen rule is why these are constants in a file of
// their own rather than incidental values scattered through the renderer: the
// whole point is that somebody else can plan around them. Docker, libvirt, k8s,
// WireGuard and mihomo all use fwmarks; mihomo and sing-box both install `ip
// rule` entries. We are not alone in any of these namespaces and must not
// behave as though we are.
//
//	fwmark        one documented byte, 0x00ff0000 — always set *and* matched
//	              with the mask, never touching another module's bits
//	RPDB priority a documented contiguous range, so a user can deliberately sit
//	              in front of or behind us
//	route tables  a documented range; never `main`, never a bare number picked
//	              at runtime
//	nftables      our own table, `olr_route`; `nft flush ruleset` is banned
const (
	// MarkMask is olr's byte of the 32-bit fwmark space.
	//
	// Bits 16–23. Low bits are where everyone else crowds — Docker uses 0x1,
	// WireGuard's wg-quick uses the table id, systemd-networkd and libvirt take
	// small values — and the top byte is conventionally left for site policy.
	MarkMask uint32 = 0x00ff0000

	// MarkShift turns a slot into a mark: slot << MarkShift.
	MarkShift = 16

	// Base is the number every other resource is offset from. One base for all
	// three namespaces is what makes a slot legible on sight.
	Base = 8100

	// MaxSlot bounds the range. Eighty exits is far past anything a box like
	// this will see, and a bound is what keeps the ranges contiguous and
	// documentable rather than "however many we ended up with".
	MaxSlot = 80

	// LocalPriority holds the rule that keeps LAN and connected traffic local.
	//
	// Load-bearing, and the single most common way a hand-rolled policy-routing
	// setup breaks (§3.3): without it an exit table's default route swallows
	// traffic addressed to your own LAN, and the symptom is that the router
	// stops being able to reach the devices behind it.
	LocalPriority = Base

	// TableName is our nftables table. Ours alone — design.md §4.2, "each
	// module writes its own table, never a shared ruleset".
	TableName = "olr_route"

	// ClassifyChain marks forwarded traffic by source.
	ClassifyChain = "classify"

	// PostroutingChain holds the per-exit SNAT of §5.3.
	PostroutingChain = "postrouting"

	// UnpolicedCounter names the counter for traffic that matched no
	// assignment, so per-exit totals can be reconciled against the box total.
	// §7.3: show what you cannot account for.
	UnpolicedCounter = "unpoliced"
)

// Slot identifies one exit's reservation across all three namespaces.
//
// **Why it is stored rather than derived.** Every alternative was tried against
// one question — what happens to traffic already in flight when the operator
// adds an exit? Deriving the slot from the exit's position in a sorted list
// means inserting "Backup" before "Clash" renumbers Clash; its route table id
// changes; and for the moment between the two netlink operations, packets whose
// `ct mark` still names the old table find nothing there and fall through to
// `main`. That is a silent direct leak of exactly the traffic the operator
// asked to route, caused by adding an unrelated row. Hashing the name avoids
// the renumber but makes the mapping unguessable, and this is a file somebody
// reads over SSH when the box is broken.
//
// So the slot is written down: allocated once, never reused while the exit
// lives, and legible next to the exit it belongs to. The three numbers it
// produces are all the same number, which is the property that matters at 2am —
// an `ip rule` line and the table it selects read alike.
//
//	slot 3  →  mark 0x00030000  →  table 8103  →  ip rule priority 8103
//
// Slot 0 is not an exit. It is the mark value of traffic no assignment matched,
// which is why the mask can be tested against zero to mean "unpoliced".
type Slot int

// Valid reports whether s is inside the documented range.
func (s Slot) Valid() bool { return s >= 1 && s <= MaxSlot }

// Mark is the fwmark value for this slot, already shifted into our byte.
func (s Slot) Mark() uint32 { return uint32(s) << MarkShift & MarkMask }

// Table is the route table id holding this exit's default route.
func (s Slot) Table() int { return Base + int(s) }

// Priority is the RPDB priority of the rule selecting that table.
func (s Slot) Priority() int { return Base + int(s) }

// Slot returns the exit's slot as the typed value.
func (e Exit) SlotID() Slot { return Slot(e.Slot) }

// Mark is the fwmark that selects this exit, for the classifier and the probe.
func (e Exit) Mark() uint32 { return e.SlotID().Mark() }

// Table is the route table holding this exit's default route.
func (e Exit) Table() int { return e.SlotID().Table() }

// Priority is the RPDB priority of this exit's rule.
func (e Exit) Priority() int { return e.SlotID().Priority() }

// OwnsPriority reports whether an RPDB priority falls in our documented range.
//
// This is the test that separates our rules from somebody else's, and it is
// used in both directions: to know which rules to replace on apply, and to know
// which ones are foreign and must be reported rather than touched (§6).
func OwnsPriority(prio int) bool {
	return prio >= LocalPriority && prio <= Base+MaxSlot
}

// OwnsTable reports whether a route table id falls in our documented range.
//
// LocalPriority's rule uses `main`, which is emphatically not ours, so the
// range starts one above Base.
func OwnsTable(id int) bool {
	return id >= Base+1 && id <= Base+MaxSlot
}

// allocateSlots gives every exit a slot, keeping the ones already assigned.
//
// The rule is "lowest free number", which is only a tie-break — it matters far
// less than what it does *not* do, which is move an exit that already has one.
// Reuse of a freed slot is deliberate and safe in a way reallocation is not:
// the exit that held it is gone, so no assignment names it and no new flow can
// be marked for it. The stale `ct mark` on an in-flight connection resolves to
// a table that now belongs to a different exit, which is the one case worth
// naming out loud — it is bounded by the lifetime of connections that existed
// before the delete, and the alternative, never reusing a number, runs the
// documented range out on a box that has been edited enough times.
func (c *Config) allocateSlots() {
	taken := make(map[int]bool, len(c.Exits))
	for i := range c.Exits {
		s := c.Exits[i].Slot
		// A duplicate or out-of-range slot is dropped rather than kept, so a
		// hand-edited file cannot produce two exits sharing a route table. It
		// is re-allocated below and Validate reports it against a path.
		if !Slot(s).Valid() || taken[s] {
			c.Exits[i].Slot = 0
			continue
		}
		taken[s] = true
	}

	next := 1
	for i := range c.Exits {
		if c.Exits[i].Slot != 0 {
			continue
		}
		for next <= MaxSlot && taken[next] {
			next++
		}
		if next > MaxSlot {
			// Out of slots. Left at zero rather than wrapping, so Validate
			// reports it as the configuration error it is instead of two exits
			// silently sharing a route table.
			return
		}
		c.Exits[i].Slot = next
		taken[next] = true
	}
}
