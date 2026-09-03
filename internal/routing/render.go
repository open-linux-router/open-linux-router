package routing

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"
)

// Rendering the kernel state a config asks for.
//
// This file is pure, and that is the load-bearing property rather than a
// stylistic one. `Render` produces a **value**, not a side effect: the planner
// diffs that value against what the kernel actually has, the differ prints it,
// the tests assert on it, and only kernel_linux.go turns it into netlink calls.
// So the whole decision layer — which prefix gets which mark, which table holds
// which route, what happens to IPv6 — is testable on a laptop with no
// privileges, which is what design.md §10 asks of every module.
//
// The rules being rendered are docs/gateway.md §3.3. The distinction that
// decides the shape is §3.1's: **nftables classifies, the RPDB routes.** There
// is no nft expression that picks a next hop — `dup` copies, `fwd` is L2-only,
// and DNAT would rewrite the destination and break the connection — so an exit
// costs one nft rule, one `ip rule`, and one route, created once and then left
// alone.

// Desired is the kernel state a config asks for, as values.
//
// Nothing here is text. Lines() renders it for the differ and for `olr diff`;
// the kernel layer walks the same fields to program them. One source, two
// readers, so a rule that is displayed is the rule that is installed.
type Desired struct {
	// Enabled is false when the module is switched off. Everything below is
	// then empty, and applying means tearing our state down and leaving the box
	// routing exactly as it did before olr was installed.
	Enabled bool

	// Table is the nftables table we own.
	Table NFTable

	// Rules are RPDB entries, in our documented priority range.
	Rules []RuleSpec

	// Routes are the per-exit default routes, in our documented table range.
	Routes []RouteSpec

	// Sysctls are the per-interface kernel settings this module owns (§5.2).
	Sysctls []SysctlSpec
}

// NFTable is the `inet olr_route` table (§3.3).
//
// The classify chain has four kinds of rule and they run in this order:
//
//	restore    an established flow gets back the exit it started on
//	source     a new flow is classified by where it came from
//	account    every packet is counted against its exit, and the decision saved
//	unpoliced  whatever matched nothing is counted too (§7.3)
//
// The restore/account pair is §3.4's `ct mark` save and restore, and it buys
// three things: an in-flight session survives a policy edit, per-flow
// classification replaces a per-packet set lookup, and editing an assignment
// stays `reload` rather than `disruptive`.
//
// Both are rendered **per exit** rather than as one generic pair, and that is
// forced rather than chosen: nftables' bitwise expression combines a register
// with a *constant* mask and xor, not with another register, so
// "set my byte of the mark to whatever the connection's byte says" is not one
// expression. Splitting it per exit makes the value a constant again. It costs
// two rules per exit in use, which is nothing, and it keeps the mask discipline
// §3.2 demands — we never write a bit outside 0x00ff0000.
type NFTable struct {
	// Exits are the in-use exits, for the restore and accounting rules.
	Exits []ExitRule

	// Sources are the classify rules: one per (assignment, family), matching a
	// source prefix and setting the exit's mark.
	Sources []SourceRule

	// SNAT are the postrouting rules of §5.3, one per next-hop exit that wants
	// its source addresses rewritten.
	SNAT []SNATRule

	// Counters are the named counter objects to create, sorted.
	//
	// Named, not anonymous inline ones, and §3.3 is emphatic about why:
	// re-rendering a chain zeroes anonymous counters, so editing one rule would
	// silently reset every other rule's numbers. Named objects survive rule
	// replacement.
	Counters []string
}

// ExitRule is the restore-and-account pair for one exit in use.
type ExitRule struct {
	Exit    string
	Mark    uint32
	Counter string
}

// RestoreLine and AccountLine are the two rules' canonical forms, each stored
// in its own rule's userdata.
func (e ExitRule) RestoreLine() string {
	return fmt.Sprintf("nft restore mark %#08x for %s", e.Mark, e.Exit)
}

func (e ExitRule) AccountLine() string {
	return fmt.Sprintf("nft account mark %#08x counter %s for %s", e.Mark, e.Counter, e.Exit)
}

// SourceRule marks traffic from one prefix for one exit.
type SourceRule struct {
	// Interface and Exit are the assignment this came from, carried for the
	// rule comment and for error messages. Neither is matched on.
	Interface string
	Exit      string

	// Prefix is the source range matched.
	Prefix netip.Prefix

	// Mark is the exit's fwmark, already shifted into our byte.
	//
	// There is deliberately no counter here. Counting happens once per exit in
	// the accounting rule, which sees every packet of every flow; counting in
	// the classify rule would count only the first packet of each connection,
	// because the restore rule handles the rest.
	Mark uint32
}

// SNATRule rewrites the source address of traffic leaving through a next hop.
type SNATRule struct {
	Exit string
	Mark uint32

	// Dev is the interface the next hop is reached through. Matching on it as
	// well as on the mark is belt and braces, and cheap.
	Dev string
}

// RuleKind distinguishes the two RPDB entries we install.
type RuleKind string

const (
	// RuleLocal is the priority-8100 entry that keeps LAN and connected
	// traffic local. One per family, always present when enabled.
	RuleLocal RuleKind = "local"

	// RuleMark selects an exit's table by its fwmark.
	RuleMark RuleKind = "mark"
)

// RuleSpec is one RPDB entry.
type RuleSpec struct {
	Kind     RuleKind
	Priority int

	// V6 selects which of the two RPDBs this belongs to. They are separate
	// kernel structures, so every rule is stated for one family explicitly
	// rather than implied for both.
	V6 bool

	// Mark and Table apply to RuleMark.
	Mark  uint32
	Table int

	// Exit is carried for messages.
	Exit string
}

// RouteType is what a route does with a packet.
type RouteType string

const (
	// RouteVia forwards it.
	RouteVia RouteType = "via"

	// RouteUnreachable refuses it with ICMP admin-prohibited, which is §3.7's
	// whole point: blackhole drops silently and every connection hangs for
	// thirty seconds, while unreachable makes applications fail immediately.
	// Same feature, completely different experience for whoever is holding the
	// tablet.
	RouteUnreachable RouteType = "unreachable"
)

// RouteSpec is one exit's default route, in that exit's own table.
type RouteSpec struct {
	Table int
	V6    bool
	Type  RouteType

	// Gateway is the next hop, nil for an interface route.
	Gateway *netip.Addr

	// Dev is the outgoing interface, empty when the kernel resolves it.
	Dev string

	// Exit is carried for messages, and Why explains an unreachable route in
	// the operator's terms — "Clash does not carry IPv6" reads very differently
	// from an unexplained refusal.
	Exit string
	Why  string
}

// SysctlSpec is one per-interface kernel setting this module owns (§5.2).
//
// Per-interface only. design.md §3.4 says shared state is touched only when a
// module explicitly owns that concern, and what this module owns is the
// interfaces it routes through — not `net.ipv4.conf.all.*`, which would change
// behaviour on interfaces nobody handed us. The one place that bites is
// `send_redirects`, where the kernel ORs the `all` value with the per-interface
// one; when `all` is 1 the plan reports it rather than quietly writing it.
type SysctlSpec struct {
	Key   string
	Value string
	Why   string
}

// Line is this rule's canonical form, and also the bytes stored in its netlink
// userdata so that reading the kernel back reproduces it exactly.
//
// Kept comfortably under the kernel's 256-byte userdata limit: an interface
// name is bounded at 15 characters, a prefix at 43, and MaxNameLen caps the
// exit. Validate rejects control characters in a name, so this is always one
// line.
func (s SourceRule) Line() string {
	return fmt.Sprintf("nft source %s %s mark %#08x from %s via %s",
		family(s.Prefix.Addr().Is6()), s.Prefix, s.Mark, s.Interface, s.Exit)
}

// Line is this rule's canonical form, stored in its userdata for the same
// reason.
func (s SNATRule) Line() string {
	dev := s.Dev
	if dev == "" {
		dev = "-"
	}
	return fmt.Sprintf("nft snat mark %#08x dev %s for %s", s.Mark, dev, s.Exit)
}

// Health is what the prober currently believes about each exit, keyed by exit
// name. An exit absent from the map is treated as up.
//
// Passed *into* rendering rather than acted on separately, and that is what
// keeps design.md §5.4 true. If the prober re-pointed a dead exit by writing to
// the kernel behind the planner's back, the next drift check would compare
// intent against a kernel that disagrees with it and report drift — for a box
// that is behaving exactly as configured. Making health an input means the
// tripped state *is* the expected state, and drift keeps meaning what it says.
type Health map[string]bool

// Down reports whether the prober has marked this exit down.
func (h Health) Down(name string) bool {
	up, known := h[name]
	return known && !up
}

// Render turns intent into the kernel state that implements it.
//
// It assumes the config has been validated; an unvalidated one produces
// nonsense rather than an error, which is why every caller validates first
// (design.md §5.3.1 — the whole value of validation is that it happens before
// anything is written).
func Render(c Config, links LinkView, health Health) Desired {
	d := Desired{Enabled: c.Enabled}
	if !c.Enabled {
		return d
	}

	// The local guard, one per family, and the most load-bearing four lines in
	// the module. Without it an exit table's default route swallows traffic
	// addressed to your own LAN, and the router stops being able to reach the
	// devices behind it (§3.3).
	d.Rules = append(d.Rules,
		RuleSpec{Kind: RuleLocal, Priority: LocalPriority, V6: false},
		RuleSpec{Kind: RuleLocal, Priority: LocalPriority, V6: true},
	)

	counters := map[string]bool{UnpolicedCounter: true}

	// Only exits something actually uses are programmed. An exit nobody routes
	// through is a saved setting, not a live route table: installing it would
	// mean a configured-but-unassigned exit still shows up in `ip rule` output,
	// which makes the kernel disagree with the screen for no gain.
	for _, e := range c.Exits {
		if !c.InUse(e.Name) {
			continue
		}
		counter := counterName(e)
		counters[counter] = true
		d.Table.Exits = append(d.Table.Exits, ExitRule{
			Exit: e.Name, Mark: e.Mark(), Counter: counter,
		})

		renderExit(&d, c, e, links, health)
	}

	renderSources(&d, c, links)

	d.Table.Counters = sortedKeys(counters)
	renderSysctls(&d, c, links)

	return d
}

// counterName is the named counter object for one exit.
//
// Keyed by slot rather than by name, so renaming an exit does not reset its
// byte count — the number an operator is watching survives them fixing a typo.
func counterName(e Exit) string { return fmt.Sprintf("exit%d", e.Slot) }

// renderExit installs one exit's RPDB rule and route, per family.
func renderExit(d *Desired, c Config, e Exit, links LinkView, health Health) {
	down := health.Down(e.Name)

	// §5.5's fail-open mode is the one case where an exit in use installs
	// nothing at all: no rule means the mark selects no table, so the traffic
	// falls through to `main` and takes the box's normal path. That it is
	// achieved by absence rather than by a route is worth knowing when reading
	// `ip rule` output on a box whose exit is down.
	if down && e.OnFailure.OrDefault() == FailDirect {
		return
	}

	for _, v6 := range []bool{false, true} {
		route, ok := exitRoute(e, links, v6, down)
		if !ok {
			// IPv6Direct: no rule, no route, so v6 takes the normal path.
			continue
		}
		route.Table = e.Table()
		route.V6 = v6
		route.Exit = e.Name

		d.Routes = append(d.Routes, route)
		d.Rules = append(d.Rules, RuleSpec{
			Kind:     RuleMark,
			Priority: e.Priority(),
			V6:       v6,
			Mark:     e.Mark(),
			Table:    e.Table(),
			Exit:     e.Name,
		})
	}

	if e.Via.Kind == ViaNextHop && e.SNATOrDefault() && !down {
		d.Table.SNAT = append(d.Table.SNAT, SNATRule{
			Exit: e.Name,
			Mark: e.Mark(),
			Dev:  nextHopDev(e, links),
		})
	}
}

// exitRoute decides the one route in an exit's table for one family.
//
// The three-way answer here is §5.4 made mechanical. An exit carries a family
// or it does not; when it does not, the traffic is refused, sent direct, or —
// never — quietly allowed to leak.
func exitRoute(e Exit, links LinkView, v6, down bool) (RouteSpec, bool) {
	if down {
		// The exit is up as far as configuration goes but the probe says it is
		// not forwarding. OnFailure is block (direct returned earlier), so
		// everything assigned to it stops here, visibly.
		return RouteSpec{
			Type: RouteUnreachable,
			Why:  fmt.Sprintf("%s is not responding", e.Name),
		}, true
	}

	if e.Via.Kind == ViaBlocked {
		return RouteSpec{
			Type: RouteUnreachable,
			Why:  fmt.Sprintf("%s blocks traffic", e.Name),
		}, true
	}

	if e.Carries(v6) {
		switch e.Via.Kind {
		case ViaInterface:
			return RouteSpec{Type: RouteVia, Dev: e.Via.Interface}, true
		case ViaNextHop:
			hop := *e.Via.NextHop
			return RouteSpec{Type: RouteVia, Gateway: &hop, Dev: nextHopDev(e, links)}, true
		}
	}

	// The exit does not carry this family. For IPv6 the operator's answer
	// decides; for IPv4 there is no field, because a v6-only next hop is exotic
	// enough that inventing one would be modelling for its own sake — it fails
	// closed, and Validate warns.
	if !v6 {
		return RouteSpec{
			Type: RouteUnreachable,
			Why:  fmt.Sprintf("%s carries IPv6 only", e.Name),
		}, true
	}

	switch e.IPv6OrDefault() {
	case IPv6Direct:
		return RouteSpec{}, false
	default:
		return RouteSpec{
			Type: RouteUnreachable,
			Why:  fmt.Sprintf("%s does not carry IPv6", e.Name),
		}, true
	}
}

// Carries reports whether this exit natively handles a family.
//
// An interface carries both — a tunnel or a TUN device is not v4-only just
// because nobody gave it a v6 address, and if it has none the route simply has
// nothing to carry. A next hop carries exactly the family of its own address,
// which is the honest answer: there is no such thing as reaching a v6
// destination through a v4 gateway.
func (e Exit) Carries(v6 bool) bool {
	switch e.Via.Kind {
	case ViaInterface:
		return true
	case ViaNextHop:
		if e.Via.NextHop == nil {
			return false
		}
		// Is4In6 is excluded deliberately: ::ffff:192.0.2.1 is an IPv4 address
		// wearing a v6 spelling, and treating it as a v6 next hop would install
		// a v6 default route that cannot carry anything.
		hopIsV6 := e.Via.NextHop.Is6() && !e.Via.NextHop.Is4In6()
		return hopIsV6 == v6
	case ViaBlocked:
		return true
	}
	return false
}

// nextHopDev resolves the interface a next hop is reached through, preferring
// what the operator wrote and falling back to the one whose subnet holds it.
//
// An empty answer is legal and means "let the kernel work it out from the main
// table", which is right whenever the hop is on a network we already have an
// address on — the case Validate has already insisted on.
func nextHopDev(e Exit, links LinkView) string {
	if e.Via.Dev != "" {
		return e.Via.Dev
	}
	if e.Via.NextHop == nil {
		return ""
	}
	if info, ok := InterfaceWithPrefixContaining(links, *e.Via.NextHop); ok {
		return info.Name
	}
	return ""
}

// renderSources builds the classify chain: one rule per assigned network per
// family, matching the network's own prefixes.
//
// The prefixes come from link rather than from our own config, which is what
// makes it impossible for the range we classify on to drift away from the
// range that is actually configured on the interface (design.md §4.1).
func renderSources(d *Desired, c Config, links LinkView) {
	for _, a := range c.Interfaces {
		exitName, _ := c.Assigned(a.Interface)
		if exitName == "" {
			// Inheriting a box default of "the box's own route". Nothing to
			// mark: unmarked traffic uses `main`, which is precisely what this
			// asks for, and a rule that set mark 0 would be a no-op that
			// somebody would later have to explain.
			continue
		}
		e, ok := c.Find(exitName)
		if !ok {
			continue // Validate has already reported this.
		}
		info, err := links.Interface(a.Interface)
		if err != nil {
			continue
		}

		for _, v6 := range []bool{false, true} {
			for _, p := range info.PrefixesFor(v6) {
				d.Table.Sources = append(d.Table.Sources, SourceRule{
					Interface: a.Interface,
					Exit:      e.Name,
					Prefix:    p.Masked(),
					Mark:      e.Mark(),
				})
			}
		}
	}

	sort.SliceStable(d.Table.Sources, func(i, j int) bool {
		return d.Table.Sources[i].Prefix.String() < d.Table.Sources[j].Prefix.String()
	})
}

// renderSysctls is §5.2, in both directions.
//
// When the exit's next hop shares a segment with the clients — the normal case,
// and the one a WAN gateway never produces — two things happen that have to be
// turned off explicitly:
//
//   - We forward a packet back out the interface it arrived on, and the kernel
//     helpfully tells the client to talk to the proxy box directly. Some clients
//     obey, and then their traffic stops passing through us at all.
//   - When the proxy box's own rules say DIRECT it forwards back out the same
//     interface and sends *us* a redirect, and our table quietly acquires routes
//     we did not choose.
func renderSysctls(d *Desired, c Config, links LinkView) {
	devs := map[string]bool{}
	for _, e := range c.Exits {
		if e.Via.Kind != ViaNextHop || !c.InUse(e.Name) {
			continue
		}
		if dev := nextHopDev(e, links); dev != "" {
			devs[dev] = true
		}
	}

	for _, dev := range sortedKeys(devs) {
		d.Sysctls = append(d.Sysctls,
			SysctlSpec{
				Key:   fmt.Sprintf("net.ipv4.conf.%s.send_redirects", dev),
				Value: "0",
				Why: "stops the kernel telling clients on " + dev +
					" to bypass this router and talk to the next hop directly",
			},
			SysctlSpec{
				Key:   fmt.Sprintf("net.ipv4.conf.%s.accept_redirects", dev),
				Value: "0",
				Why: "stops the next hop adding routes to this box's table that " +
					"nobody configured",
			},
			SysctlSpec{
				Key:   fmt.Sprintf("net.ipv6.conf.%s.accept_redirects", dev),
				Value: "0",
				Why:   "the same, for IPv6",
			},
		)
	}
}

// --- canonical text -------------------------------------------------------

// Lines renders the desired state as canonical text.
//
// One representation does three jobs, and that is deliberate rather than
// thrifty. It is the diff basis, so `olr diff` shows a routing change as lines
// rather than as "3 things will change". It is what the UI prints. And each nft
// line is stored verbatim in that rule's netlink userdata, so reading the
// kernel back yields exactly these strings and drift is a string comparison —
// no re-deriving a rule from its expression list, and no second format that
// could disagree with the first.
//
// That last job is why these are compact semantic lines rather than nft syntax.
// Rendering `ip saddr … meta mark set meta mark and 0xff00ffff or 0x00020000`
// here would mean writing the ruleset twice, once for humans and once for the
// kernel, with nothing keeping the two honest — and it would not fit in the 256
// bytes the kernel allows for userdata. The nft syntax lives in exactly one
// place, kernel_linux.go, where it is executed rather than described.
func (d Desired) Lines() []string {
	out := d.objectLines()
	for _, s := range d.Sysctls {
		out = append(out, fmt.Sprintf("sysctl %s = %s", s.Key, s.Value))
	}
	return out
}

// objectLines is the subset the planner compares against the kernel: the things
// we create and would delete.
//
// Sysctls are excluded and handled separately, because they are the one thing
// here we edit rather than own. Every other line describes an object that
// exists because we made it, so its absence from the desired set means "remove
// it"; a sysctl somebody else had already set to 0 is not ours to put back.
func (d Desired) objectLines() []string {
	if !d.Enabled {
		return nil
	}

	var out []string

	out = append(out, fmt.Sprintf("nft table inet %s", TableName))
	out = append(out, fmt.Sprintf("nft chain %s prerouting mangle", ClassifyChain))
	for _, name := range d.Table.Counters {
		out = append(out, fmt.Sprintf("nft counter %s", name))
	}

	// In evaluation order, which is also the order they are installed, because
	// a reader comparing this against `nft list ruleset` should be able to
	// follow a packet down the list.
	for _, e := range d.Table.Exits {
		out = append(out, e.RestoreLine())
	}
	for _, s := range d.Table.Sources {
		out = append(out, s.Line())
	}
	for _, e := range d.Table.Exits {
		out = append(out, e.AccountLine())
	}
	out = append(out, "nft count-unpoliced")

	if len(d.Table.SNAT) > 0 {
		out = append(out, fmt.Sprintf("nft chain %s postrouting srcnat", PostroutingChain))
		for _, s := range d.Table.SNAT {
			out = append(out, s.Line())
		}
	}

	for _, r := range d.Rules {
		out = append(out, r.Line())
	}
	for _, r := range d.Routes {
		out = append(out, r.Line())
	}

	return out
}

// Line is this RPDB entry's canonical form.
//
// A method rather than a local format string because the kernel layer keys its
// add/remove sets on exactly this text: if the two spellings drifted apart,
// every apply would remove a rule and immediately add back an identical one,
// and every plan would report work that had already been done.
func (r RuleSpec) Line() string {
	if r.Kind == RuleLocal {
		return fmt.Sprintf("rule %s priority %d from all lookup main suppress_prefixlength 0",
			family(r.V6), r.Priority)
	}
	return fmt.Sprintf("rule %s priority %d fwmark %#08x/%#08x lookup %d",
		family(r.V6), r.Priority, r.Mark, MarkMask, r.Table)
}

// Line is this route's canonical form, for the same reason.
func (r RouteSpec) Line() string {
	if r.Type == RouteUnreachable {
		return fmt.Sprintf("route %s table %d unreachable default", family(r.V6), r.Table)
	}
	line := fmt.Sprintf("route %s table %d default", family(r.V6), r.Table)
	if r.Gateway != nil {
		line += " via " + r.Gateway.String()
	}
	if r.Dev != "" {
		line += " dev " + r.Dev
	}
	return line
}

// Text renders Lines as a document, which is what the differ compares.
func (d Desired) Text() string {
	lines := d.Lines()
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

func family(v6 bool) string {
	if v6 {
		return "ip6"
	}
	return "ip"
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
