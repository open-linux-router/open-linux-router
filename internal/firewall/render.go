package firewall

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"
)

// Rendering the kernel state a config asks for.
//
// This file is pure, and that is the load-bearing property rather than a
// stylistic one. Render produces a **value**, not a side effect: the planner
// diffs that value against what the kernel actually has, the differ prints it,
// the tests assert on it, and only kernel_linux.go turns it into netlink calls.
// So the whole decision layer — which interface a rule matches, which source
// ranges get masqueraded, what a range may be remapped to — is testable on a
// laptop with no privileges, which is what design.md §10 asks of every module.
//
// The rules being rendered are docs/firewall.md §3.1.

// Desired is the kernel state a config asks for, as values.
//
// Nothing here is text. Lines() renders it for the differ and for `olr diff`;
// the kernel layer walks the same fields to program them. One source, two
// readers, so a rule that is displayed is the rule that is installed.
type Desired struct {
	// Enabled is false when the module is switched off. Table is then empty,
	// and applying means removing our table and leaving the box translating
	// exactly as it did before olr was installed.
	Enabled bool

	// Table is the nftables table we own.
	Table NFTable
}

// NFTable is the `inet olr_nat` table (docs/firewall.md §3.1).
//
// Two chains and three kinds of rule:
//
//	dnat      a connection from outside, redirected inward
//	hairpin   the same connection, opened from inside instead (§4)
//	masq      the other half of the hairpin, so replies come back through us
//
// The hairpin pair is one feature described in two chains, and neither half
// works alone: without the DNAT a LAN client reaches the router and is refused,
// and without the masquerade the device replies directly over shared L2 and the
// client drops a reply from an address it never sent to.
type NFTable struct {
	// DNAT are the inbound translations, one per (forward, protocol).
	DNAT []DNATRule

	// Hairpin are the inside-facing copies, one per (forward, protocol, inside
	// interface).
	Hairpin []HairpinRule

	// Masq are the postrouting rules that bring hairpinned replies back through
	// this box, one per (forward, protocol, source prefix).
	Masq []MasqRule

	// Counters are the named counter objects to create, sorted.
	//
	// Named, not anonymous inline ones, and docs/firewall.md §3.4 is emphatic
	// about why: this module replaces its whole table on every apply, so an
	// anonymous counter would be zeroed every time — adding a second forward
	// would silently reset the first one's numbers.
	Counters []string
}

// DNATRule is one inbound translation: a connection arriving on In, to this
// router's own address, on Port, goes to To instead.
type DNATRule struct {
	// Forward is the operator's name for this, carried for the rule comment and
	// for error messages. It is not matched on.
	Forward string

	// In is the interface the connection arrives on.
	In string

	// Protocol is tcp or udp, never both: Protocol.Each has already expanded it,
	// so what reaches here is always one transport and one rule.
	Protocol Protocol

	// Port is the port, or ports, matched on arrival.
	Port PortRange

	// To is the address delivered to, and ToPorts the port or ports there.
	//
	// Separate from a single AddrPort because a range forward translates to a
	// range: the kernel is given `192.168.1.10:30000-30010` and keeps each
	// connection's own port, which is only exact because Validate has already
	// insisted the two ranges are identical (docs/firewall.md §1.3).
	To      netip.Addr
	ToPorts PortRange

	// Counter is the named counter object this rule increments.
	Counter string
}

// HairpinRule is the same translation for a connection opened from inside the
// network (docs/firewall.md §4).
//
// A separate type from DNATRule rather than a flag on it, because of the one
// field it does not have: **no counter.** The question the counter answers is
// *did anything from outside actually arrive* — the first thing worth knowing
// when a port does not work — and a hairpinned connection from the laptop in the
// next room would answer it yes while the port was shut to the world. Counting
// both would make the number agree with the operator's test and disagree with
// reality, which is the one way a diagnostic can be worse than absent.
type HairpinRule struct {
	Forward string

	// In is the inside interface the connection arrives on.
	In string

	Protocol Protocol
	Port     PortRange
	To       netip.Addr
	ToPorts  PortRange
}

// MasqRule rewrites the source address of a hairpinned connection to this
// router's, so that the device replies through us rather than directly.
//
// Without it the device at To is on the same segment as the client, replies
// straight to it, and the client drops a reply from an address it never sent to
// — a connection that hangs rather than failing, which is worse. What it costs
// is that the device cannot tell its own clients apart; Validate warns about
// that on every forward it applies to.
type MasqRule struct {
	Forward string

	// Prefix is the source range whose replies would otherwise bypass us: a
	// network of ours that holds the destination address.
	Prefix netip.Prefix

	// To and Ports are the translated destination, matched so that the
	// masquerade applies to this forward's traffic and nothing else on the
	// segment.
	To    netip.Addr
	Ports PortRange

	Protocol Protocol
}

// Render turns intent into the kernel state that implements it.
//
// It assumes the config has been validated; an unvalidated one produces nonsense
// rather than an error, which is why every caller validates first (design.md
// §5.3.1 — the whole value of validation is that it happens before anything is
// written).
func Render(c Config, links LinkView) Desired {
	d := Desired{Enabled: c.Enabled}
	if !c.Enabled {
		return d
	}

	counters := map[string]bool{}

	for _, f := range c.Forwards {
		to := f.To.Addr().Unmap()
		inside := f.InsidePorts()
		counter := f.Counter()
		counters[counter] = true

		for _, proto := range f.ProtocolOrDefault().Each() {
			d.Table.DNAT = append(d.Table.DNAT, DNATRule{
				Forward:  f.Name,
				In:       f.In,
				Protocol: proto,
				Port:     f.Port,
				To:       to,
				ToPorts:  inside,
				Counter:  counter,
			})

			if !f.HairpinOrDefault() {
				continue
			}

			// One rule per inside interface rather than one rule with an
			// interface set. Sets would be fewer rules and more machinery, and
			// the thing actually being optimised here is whether somebody
			// reading `nft list table inet olr_nat` at 2am can see which
			// networks the forward answers on. A handful of extra rules on a
			// box with three interfaces is not a cost worth obscuring that for.
			for _, in := range InsideInterfaces(links, f.In) {
				d.Table.Hairpin = append(d.Table.Hairpin, HairpinRule{
					Forward:  f.Name,
					In:       in.Name,
					Protocol: proto,
					Port:     f.Port,
					To:       to,
					ToPorts:  inside,
				})
			}

			// The masquerade half, and only for the networks that need it: a
			// source range that holds the destination is one where the reply
			// would otherwise go straight back over shared L2. A client on a
			// different network already replies through this router, so
			// masquerading it would hide the real source address for nothing.
			for _, prefix := range PrefixesContaining(links, to) {
				d.Table.Masq = append(d.Table.Masq, MasqRule{
					Forward:  f.Name,
					Prefix:   prefix,
					To:       to,
					Ports:    inside,
					Protocol: proto,
				})
			}
		}
	}

	sortRules(&d.Table)
	d.Table.Counters = sortedKeys(counters)
	return d
}

// InsidePorts is the port or ports the connection is delivered to.
//
// A single port may be remapped freely, so it is whatever `to` says. A range may
// only go to the identical range, which Validate enforces — so the answer here
// is the arriving range itself, and the two can never disagree because only one
// of them is ever read (docs/firewall.md §1.3).
func (f Forward) InsidePorts() PortRange {
	if f.Port.Single() {
		return SinglePort(f.To.Port())
	}
	return f.Port
}

// sortRules puts every list in a stable order.
//
// Not cosmetic: these lines are diffed as sets by the planner, but they are
// *installed* in slice order, so an unstable order would make the rendered
// ruleset churn between applies for no reason — and `nft list table` output
// would reorder itself under a reader trying to compare two boxes.
func sortRules(t *NFTable) {
	sort.SliceStable(t.DNAT, func(i, j int) bool { return t.DNAT[i].Line() < t.DNAT[j].Line() })
	sort.SliceStable(t.Hairpin, func(i, j int) bool { return t.Hairpin[i].Line() < t.Hairpin[j].Line() })
	sort.SliceStable(t.Masq, func(i, j int) bool { return t.Masq[i].Line() < t.Masq[j].Line() })
}

// --- canonical text -------------------------------------------------------

// Line is this rule's canonical form, and also the bytes stored in its netlink
// userdata so that reading the kernel back reproduces it exactly.
//
// One representation does three jobs, the same way internal/gateway's does. It
// is the diff basis, so `olr diff` shows a forwarding change as lines rather
// than as "3 things will change". It is what the UI prints. And each line is
// stored verbatim in that rule's userdata in nft's own comment format, so
// reading the kernel back yields exactly these strings and drift is a string
// comparison — no re-deriving a rule from its expression list, and no second
// format that could disagree with the first.
//
// That last job is why these are compact semantic lines rather than nft syntax.
// Rendering the real expression list here would mean writing the ruleset twice,
// once for humans and once for the kernel, with nothing keeping the two honest —
// and it would not fit in the 256 bytes the kernel allows for userdata. The nft
// syntax lives in exactly one place, kernel_linux.go, where it is executed
// rather than described.
//
// Comfortably under the limit: an interface name is bounded at 15 characters, an
// address at 15, a port range at 11, and MaxNameLen caps the forward. Validate
// rejects control characters in a name, so this is always one line.
func (r DNATRule) Line() string {
	return fmt.Sprintf("nft dnat in %s %s port %s to %s counter %s for %s",
		r.In, r.Protocol, r.Port, target(r.To, r.ToPorts), r.Counter, r.Forward)
}

// Line is this rule's canonical form, stored in its userdata for the same
// reason. The absence of a counter is visible in the text, which is what makes
// an inbound rule and a hairpin rule distinguishable in `nft list ruleset`
// rather than only in this package.
func (r HairpinRule) Line() string {
	return fmt.Sprintf("nft hairpin in %s %s port %s to %s for %s",
		r.In, r.Protocol, r.Port, target(r.To, r.ToPorts), r.Forward)
}

// Line is this rule's canonical form, stored in its userdata for the same
// reason.
func (r MasqRule) Line() string {
	return fmt.Sprintf("nft masq from %s to %s %s port %s for %s",
		r.Prefix, r.To, r.Protocol, r.Ports, r.Forward)
}

// target spells an address and its ports the way nft does, so the canonical line
// reads like the rule it describes.
func target(addr netip.Addr, ports PortRange) string {
	return fmt.Sprintf("%s:%s", addr, ports)
}

// Lines renders the desired state as canonical text.
func (d Desired) Lines() []string { return d.objectLines() }

// objectLines is the set the planner compares against the kernel: the things we
// create and would delete.
//
// Every line here describes an object that exists because we made it, so its
// absence from the desired set means "remove it". This module has no equivalent
// of internal/gateway's sysctls — nothing it edits that the box already had —
// which is why Lines and objectLines are the same list rather than two.
func (d Desired) objectLines() []string {
	if !d.Enabled {
		return nil
	}

	out := []string{
		fmt.Sprintf("nft table inet %s", TableName),
		fmt.Sprintf("nft chain %s prerouting dstnat", PreroutingChain),
	}
	for _, name := range d.Table.Counters {
		out = append(out, fmt.Sprintf("nft counter %s", name))
	}

	// In evaluation order, which is also the order they are installed, because
	// a reader comparing this against `nft list ruleset` should be able to
	// follow a packet down the list.
	for _, r := range d.Table.DNAT {
		out = append(out, r.Line())
	}
	for _, r := range d.Table.Hairpin {
		out = append(out, r.Line())
	}

	if len(d.Table.Masq) > 0 {
		out = append(out, fmt.Sprintf("nft chain %s postrouting srcnat", PostroutingChain))
		for _, r := range d.Table.Masq {
			out = append(out, r.Line())
		}
	}

	return out
}

// Text renders Lines as a document, which is what the differ compares.
func (d Desired) Text() string {
	lines := d.Lines()
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
