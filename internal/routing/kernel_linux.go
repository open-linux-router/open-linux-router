//go:build linux

package routing

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strconv"
	"strings"

	"github.com/google/nftables"
	"github.com/google/nftables/binaryutil"
	"github.com/google/nftables/expr"
	"github.com/google/nftables/userdata"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

// The kernel half, and the only file in this module that knows what netlink is.
//
// Everything above it works in values: Render decides, BuildPlan compares, and
// this translates. That is what design.md §10 asks for under test hardware, and
// it is load-bearing here in a way it is not for the file-rendering modules —
// what this module configures *is* the kernel, so without the seam there would
// be nothing testable off a router at all.
//
// There is no `exec.Command` here and there never may be (design.md §3.6): the
// sandbox in olrd.service is nearly free precisely because the process spawns
// nothing, and the first shell-out silently costs all of it.

// LinuxKernel programs nftables and the routing policy database.
type LinuxKernel struct {
	// ProcRoot prefixes /proc, for tests and for the --root development mode.
	// Empty means the real one.
	ProcRoot string
}

// NewKernel returns the kernel implementation for this platform.
func NewKernel() Kernel { return LinuxKernel{} }

// --- observation ----------------------------------------------------------

// Observe reads the live state into the same canonical lines Render produces.
//
// Every part is read fresh from the kernel. Nothing here consults a cache of
// what we last wrote, which is what makes drift detection mean something: a
// hand-run `ip rule del` has to show up the same way a hand-edited config file
// does in the other modules (design.md §5.4, §4.5).
func (k LinuxKernel) Observe(_ context.Context) (Observed, error) {
	obs := Observed{Known: true, Sysctls: map[string]string{}}

	nftLines, err := k.observeNFT()
	if err != nil {
		return Observed{}, fmt.Errorf("reading nftables: %w", err)
	}
	obs.Lines = append(obs.Lines, nftLines...)

	ruleLines, foreign, err := k.observeRules()
	if err != nil {
		return Observed{}, fmt.Errorf("reading ip rules: %w", err)
	}
	obs.Lines = append(obs.Lines, ruleLines...)
	obs.Foreign = foreign

	routeLines, err := k.observeRoutes()
	if err != nil {
		return Observed{}, fmt.Errorf("reading routes: %w", err)
	}
	obs.Lines = append(obs.Lines, routeLines...)

	obs.Sysctls = k.readSysctls()
	if v, ok := k.readSysctl("net.ipv4.conf.all.send_redirects"); ok {
		on := v != "0"
		obs.AllSendRedirects = &on
	}

	obs.Active = k.observeActive()

	return obs, nil
}

// observeNFT reads our table back.
//
// Each rule carries its own canonical line in netlink userdata, in nft's
// comment format, so this is a read rather than a reconstruction — and
// `nft list ruleset` shows the same text `olr diff` does, which is worth more
// than it costs.
//
// **The known gap, stated rather than left to be found.** A rule whose
// expressions were changed by hand while its comment was left intact reads back
// as unchanged, so that specific edit is not detected as drift. Reconstructing
// a rule from its expression list is what would close it, and that is the
// nftables reader design.md §4.2 gives to the `firewall` module for the whole
// box rather than to each module for its own table. The cost is bounded in the
// meantime: every apply replaces the table wholesale in one transaction, so a
// hand-edit is *corrected* on the next apply even while it goes unreported.
func (k LinuxKernel) observeNFT() ([]string, error) {
	conn, err := nftables.New()
	if err != nil {
		return nil, err
	}
	defer conn.CloseLasting()

	tables, err := conn.ListTablesOfFamily(nftables.TableFamilyINet)
	if err != nil {
		return nil, err
	}
	var table *nftables.Table
	for _, t := range tables {
		if t.Name == TableName {
			table = t
			break
		}
	}
	if table == nil {
		return nil, nil
	}

	out := []string{fmt.Sprintf("nft table inet %s", TableName)}

	objs, err := conn.GetObjects(table)
	if err != nil {
		return nil, err
	}
	for _, o := range objs {
		if c, ok := o.(*nftables.CounterObj); ok {
			out = append(out, fmt.Sprintf("nft counter %s", c.Name))
		}
	}

	chains, err := conn.ListChainsOfTableFamily(nftables.TableFamilyINet)
	if err != nil {
		return nil, err
	}
	for _, c := range chains {
		if c.Table == nil || c.Table.Name != TableName {
			continue
		}
		switch c.Name {
		case ClassifyChain:
			out = append(out, fmt.Sprintf("nft chain %s prerouting mangle", ClassifyChain))
		case PostroutingChain:
			out = append(out, fmt.Sprintf("nft chain %s postrouting srcnat", PostroutingChain))
		default:
			continue
		}

		rules, err := conn.GetRules(table, c)
		if err != nil {
			return nil, err
		}
		for _, r := range rules {
			if line, ok := userdata.GetString(r.UserData, userdata.TypeComment); ok {
				out = append(out, line)
			} else {
				// A rule in our table that we did not label. Reported as an
				// unrecognised line so it shows up as something to remove,
				// rather than being silently tolerated in a table we claim to
				// own outright.
				out = append(out, fmt.Sprintf("nft unrecognised rule in %s", c.Name))
			}
		}
	}

	return out, nil
}

// reservedTables are the kernel's own, which are never foreign and never ours.
func reservedTable(id int) bool {
	switch id {
	case unix.RT_TABLE_UNSPEC, unix.RT_TABLE_DEFAULT, unix.RT_TABLE_MAIN, unix.RT_TABLE_LOCAL:
		return true
	}
	return false
}

// observeRules reads the RPDB: ours as canonical lines, everybody else's as
// candidates for §6's refusal.
func (k LinuxKernel) observeRules() (lines []string, foreign []ForeignRule, err error) {
	defaults, err := k.tablesWithDefaultRoute()
	if err != nil {
		return nil, nil, err
	}

	for _, fam := range []int{netlink.FAMILY_V4, netlink.FAMILY_V6} {
		v6 := fam == netlink.FAMILY_V6
		rules, err := netlink.RuleList(fam)
		if err != nil {
			return nil, nil, err
		}
		for _, r := range rules {
			if OwnsPriority(r.Priority) {
				lines = append(lines, observedRuleLine(r, v6))
				continue
			}
			if reservedTable(r.Table) || OwnsTable(r.Table) {
				continue
			}
			// §6, and the test is structural on purpose. mihomo and sing-box
			// both move their priority numbers between versions, so a check
			// that named one would pass on exactly the release that broke it.
			// What makes a rule a conflict is not where it sits but what it
			// does: selecting a table that carries a default route is being a
			// second owner of "where does traffic go".
			if !defaults[r.Table] {
				continue
			}
			foreign = append(foreign, ForeignRule{
				Priority:   r.Priority,
				Family:     family(v6),
				Table:      r.Table,
				Selector:   strings.TrimSpace(r.String()),
				HasDefault: true,
			})
		}
	}
	return lines, foreign, nil
}

// observedRuleLine renders one of our own RPDB entries.
//
// Anything in our priority range that does not match a shape we install is
// rendered as unrecognised, so it diffs as a removal. That is deliberate: the
// range is documented as ours, and leaving a stray rule inside it would mean
// the kernel and the screen disagree about what is in force.
func observedRuleLine(r netlink.Rule, v6 bool) string {
	switch {
	case r.Priority == LocalPriority && r.SuppressPrefixlen == 0 && r.Table == unix.RT_TABLE_MAIN:
		return fmt.Sprintf("rule %s priority %d from all lookup main suppress_prefixlength 0",
			family(v6), r.Priority)
	case r.Mask != nil && *r.Mask == MarkMask:
		return fmt.Sprintf("rule %s priority %d fwmark %#08x/%#08x lookup %d",
			family(v6), r.Priority, r.Mark, MarkMask, r.Table)
	default:
		return fmt.Sprintf("rule %s priority %d unrecognised", family(v6), r.Priority)
	}
}

// tablesWithDefaultRoute reports which route tables carry a default route.
//
// One pass over every table in both families rather than a query per candidate:
// a box running Docker and a VPN can easily have a dozen, and the answer is the
// same shape either way.
func (k LinuxKernel) tablesWithDefaultRoute() (map[int]bool, error) {
	out := map[int]bool{}
	for _, fam := range []int{netlink.FAMILY_V4, netlink.FAMILY_V6} {
		routes, err := allRoutes(fam)
		if err != nil {
			return nil, err
		}
		for _, r := range routes {
			if isDefault(r) {
				out[r.Table] = true
			}
		}
	}
	return out, nil
}

// allRoutes lists routes across every table, which is `ip route show table
// all`. RT_TABLE_UNSPEC as the filter value is how that is spelled over
// netlink.
func allRoutes(fam int) ([]netlink.Route, error) {
	return netlink.RouteListFiltered(fam,
		&netlink.Route{Table: unix.RT_TABLE_UNSPEC}, netlink.RT_FILTER_TABLE)
}

func isDefault(r netlink.Route) bool {
	if r.Dst == nil {
		return true
	}
	ones, _ := r.Dst.Mask.Size()
	return ones == 0
}

// observeRoutes reads the default route in each of our tables.
func (k LinuxKernel) observeRoutes() ([]string, error) {
	var out []string
	for _, fam := range []int{netlink.FAMILY_V4, netlink.FAMILY_V6} {
		v6 := fam == netlink.FAMILY_V6
		routes, err := allRoutes(fam)
		if err != nil {
			return nil, err
		}
		for _, r := range routes {
			if !OwnsTable(r.Table) || !isDefault(r) {
				continue
			}
			out = append(out, observedRouteLine(r, v6))
		}
	}
	return out, nil
}

func observedRouteLine(r netlink.Route, v6 bool) string {
	if r.Type == unix.RTN_UNREACHABLE {
		return fmt.Sprintf("route %s table %d unreachable default", family(v6), r.Table)
	}
	line := fmt.Sprintf("route %s table %d default", family(v6), r.Table)
	if r.Gw != nil {
		if a, ok := netip.AddrFromSlice(r.Gw); ok {
			line += " via " + a.Unmap().String()
		}
	}
	if r.LinkIndex > 0 {
		if link, err := netlink.LinkByIndex(r.LinkIndex); err == nil {
			line += " dev " + link.Attrs().Name
		}
	}
	return line
}

// observeActive reports which neighbours have recently exchanged traffic.
//
// This is what makes `disruptive` a fact rather than a guess (§5.3.3), and it
// is worth being precise about what it does and does not prove. REACHABLE,
// DELAY and PROBE mean the kernel has confirmed forward progress to that
// address recently; STALE is deliberately excluded, because it only means the
// entry existed once, and counting it would make every plan on a box with a
// long neighbour table claim it was about to disconnect somebody. Erring toward
// silence here is the right direction: a classification that cried wolf would
// train the operator to click through the one dialog that matters.
func (k LinuxKernel) observeActive() []netip.Addr {
	var out []netip.Addr
	for _, fam := range []int{netlink.FAMILY_V4, netlink.FAMILY_V6} {
		neighs, err := netlink.NeighList(0, fam)
		if err != nil {
			continue
		}
		for _, n := range neighs {
			if n.State&(netlink.NUD_REACHABLE|netlink.NUD_DELAY|netlink.NUD_PROBE) == 0 {
				continue
			}
			if a, ok := netip.AddrFromSlice(n.IP); ok {
				out = append(out, a.Unmap())
			}
		}
	}
	return out
}

// --- sysctls --------------------------------------------------------------

// sysctlPath turns a dotted key into its /proc/sys path.
func (k LinuxKernel) sysctlPath(key string) string {
	return k.ProcRoot + "/proc/sys/" + strings.ReplaceAll(key, ".", "/")
}

func (k LinuxKernel) readSysctl(key string) (string, bool) {
	data, err := os.ReadFile(k.sysctlPath(key))
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(data)), true
}

// readSysctls reads the per-interface settings this module owns, for every
// interface that has them.
//
// Read broadly, write narrowly (design.md §3.4): we look at every interface so
// the plan can say something true about the ones we are about to touch, and we
// only ever write to the ones an exit actually uses.
func (k LinuxKernel) readSysctls() map[string]string {
	out := map[string]string{}
	links, err := netlink.LinkList()
	if err != nil {
		return out
	}
	for _, l := range links {
		name := l.Attrs().Name
		for _, key := range []string{
			"net.ipv4.conf." + name + ".send_redirects",
			"net.ipv4.conf." + name + ".accept_redirects",
			"net.ipv6.conf." + name + ".accept_redirects",
		} {
			if v, ok := k.readSysctl(key); ok {
				out[key] = v
			}
		}
	}
	return out
}

func (k LinuxKernel) writeSysctl(key, value string) error {
	return os.WriteFile(k.sysctlPath(key), []byte(value+"\n"), 0o644)
}

// --- apply ----------------------------------------------------------------

// Apply programs the desired state.
//
// The order is ApplyOrder's, and every intermediate state it passes through is
// one where traffic is either handled correctly or not classified at all:
//
//	add routes  →  add rules  →  replace nftables  →  drop stale rules  →  drop stale routes
//
// Additions go bottom-up so a mark never names a table that has no route yet;
// removals go top-down so a route is never withdrawn while a rule still points
// at it. Get either backwards and there is a window of milliseconds in which
// the RPDB falls through to `main` and the traffic the operator asked to route
// leaves direct — silently, which is the combination this module works hardest
// to avoid.
func (k LinuxKernel) Apply(ctx context.Context, d Desired) ([]Step, error) {
	var steps []Step

	run := func(desc string, fn func() error) error {
		if err := fn(); err != nil {
			steps = append(steps, Step{Description: desc, Error: err.Error()})
			return err
		}
		steps = append(steps, Step{Description: desc, Done: true})
		return nil
	}

	wantRoutes, err := desiredRoutes(d)
	if err != nil {
		return []Step{{Description: "prepare routes", Error: err.Error()}}, err
	}
	haveRoutes, err := k.currentRoutes()
	if err != nil {
		return []Step{{Description: "read current routes", Error: err.Error()}}, err
	}
	wantRules := desiredRules(d)
	haveRules, err := k.currentRules()
	if err != nil {
		return []Step{{Description: "read current ip rules", Error: err.Error()}}, err
	}

	addRoutes, delRoutes := diffRoutes(haveRoutes, wantRoutes)
	addRules, delRules := diffRules(haveRules, wantRules)

	if len(addRoutes) > 0 {
		if err := run(fmt.Sprintf("add %s", plural(len(addRoutes), "route")), func() error {
			return applyRoutes(addRoutes, netlink.RouteReplace)
		}); err != nil {
			return steps, err
		}
	}

	if len(addRules) > 0 {
		if err := run(fmt.Sprintf("add %s", plural(len(addRules), "ip rule")), func() error {
			return applyRules(addRules, netlink.RuleAdd)
		}); err != nil {
			return steps, err
		}
	}

	// One transaction. design.md §5.2 leans on exactly this: an nftables load
	// is a single all-or-nothing netlink batch, so no partial ruleset is ever
	// visible and there is nothing for a userspace two-phase commit to add.
	if err := run("write the nftables table", func() error { return k.applyNFT(d) }); err != nil {
		return steps, err
	}

	if len(delRules) > 0 {
		if err := run(fmt.Sprintf("remove %s", plural(len(delRules), "stale ip rule")), func() error {
			return applyRules(delRules, netlink.RuleDel)
		}); err != nil {
			return steps, err
		}
	}

	if len(delRoutes) > 0 {
		if err := run(fmt.Sprintf("remove %s", plural(len(delRoutes), "stale route")), func() error {
			return applyRoutes(delRoutes, netlink.RouteDel)
		}); err != nil {
			return steps, err
		}
	}

	for _, s := range d.Sysctls {
		if v, ok := k.readSysctl(s.Key); ok && v == s.Value {
			continue
		}
		if err := run("set "+s.Key+" to "+s.Value, func() error {
			return k.writeSysctl(s.Key, s.Value)
		}); err != nil {
			return steps, err
		}
	}

	_ = ctx
	return steps, nil
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}

// --- routes ---------------------------------------------------------------

// desiredRoutes turns RouteSpecs into netlink routes, keyed by canonical line
// so that comparison is a string comparison and cannot disagree with the plan.
func desiredRoutes(d Desired) (map[string]*netlink.Route, error) {
	out := map[string]*netlink.Route{}
	for _, r := range d.Routes {
		route := &netlink.Route{
			Table:    r.Table,
			Protocol: unix.RTPROT_STATIC,
		}
		if r.V6 {
			route.Family = netlink.FAMILY_V6
			route.Dst = &net.IPNet{IP: net.IPv6zero, Mask: net.CIDRMask(0, 128)}
		} else {
			route.Family = netlink.FAMILY_V4
			route.Dst = &net.IPNet{IP: net.IPv4zero, Mask: net.CIDRMask(0, 32)}
		}

		switch r.Type {
		case RouteUnreachable:
			// unreachable, never blackhole (§3.7). Blackhole drops silently and
			// every connection hangs for thirty seconds; unreachable sends ICMP
			// admin-prohibited and applications fail at once. Same feature,
			// completely different experience for whoever is holding the tablet.
			route.Type = unix.RTN_UNREACHABLE
		default:
			route.Type = unix.RTN_UNICAST
			if r.Gateway != nil {
				route.Gw = net.IP(r.Gateway.AsSlice())
			}
			if r.Dev != "" {
				link, err := netlink.LinkByName(r.Dev)
				if err != nil {
					return nil, fmt.Errorf("exit %q names interface %q, which is not present: %w",
						r.Exit, r.Dev, err)
				}
				route.LinkIndex = link.Attrs().Index
			}
		}
		// Keyed by the same canonical line the plan showed the operator, so
		// "what the plan said" and "what gets programmed" cannot come apart.
		out[r.Line()] = route
	}
	return out, nil
}

func (k LinuxKernel) currentRoutes() (map[string]*netlink.Route, error) {
	out := map[string]*netlink.Route{}
	for _, fam := range []int{netlink.FAMILY_V4, netlink.FAMILY_V6} {
		routes, err := allRoutes(fam)
		if err != nil {
			return nil, err
		}
		for i, r := range routes {
			if !OwnsTable(r.Table) || !isDefault(r) {
				continue
			}
			out[observedRouteLine(r, fam == netlink.FAMILY_V6)] = &routes[i]
		}
	}
	return out, nil
}

func diffRoutes(have, want map[string]*netlink.Route) (add, del []*netlink.Route) {
	for line, r := range want {
		if _, ok := have[line]; !ok {
			add = append(add, r)
		}
	}
	for line, r := range have {
		if _, ok := want[line]; !ok {
			del = append(del, r)
		}
	}
	return add, del
}

func applyRoutes(routes []*netlink.Route, fn func(*netlink.Route) error) error {
	for _, r := range routes {
		if err := fn(r); err != nil {
			return fmt.Errorf("table %d: %w", r.Table, err)
		}
	}
	return nil
}

// --- ip rules -------------------------------------------------------------

func desiredRules(d Desired) map[string]*netlink.Rule {
	out := map[string]*netlink.Rule{}
	for _, r := range d.Rules {
		rule := netlink.NewRule()
		rule.Priority = r.Priority
		if r.V6 {
			rule.Family = netlink.FAMILY_V6
		} else {
			rule.Family = netlink.FAMILY_V4
		}

		switch r.Kind {
		case RuleLocal:
			rule.Table = unix.RT_TABLE_MAIN
			rule.SuppressPrefixlen = 0
		case RuleMark:
			rule.Table = r.Table
			rule.Mark = r.Mark
			mask := MarkMask
			// Always matched *with* the mask, never bare (§3.2). A rule
			// matching the whole fwmark would also match on Docker's or
			// WireGuard's bits, and would stop matching the moment one of them
			// set a bit of its own.
			rule.Mask = &mask
		}
		out[r.Line()] = rule
	}
	return out
}

func (k LinuxKernel) currentRules() (map[string]*netlink.Rule, error) {
	out := map[string]*netlink.Rule{}
	for _, fam := range []int{netlink.FAMILY_V4, netlink.FAMILY_V6} {
		rules, err := netlink.RuleList(fam)
		if err != nil {
			return nil, err
		}
		for i, r := range rules {
			if !OwnsPriority(r.Priority) {
				continue
			}
			rule := rules[i]
			// RuleList does not set Family on the way out, and RuleDel needs
			// it: without this every removal would be attempted against IPv4
			// and the IPv6 rules would survive forever.
			rule.Family = fam
			out[observedRuleLine(r, fam == netlink.FAMILY_V6)] = &rule
		}
	}
	return out, nil
}

func diffRules(have, want map[string]*netlink.Rule) (add, del []*netlink.Rule) {
	for line, r := range want {
		if _, ok := have[line]; !ok {
			add = append(add, r)
		}
	}
	for line, r := range have {
		if _, ok := want[line]; !ok {
			del = append(del, r)
		}
	}
	return add, del
}

func applyRules(rules []*netlink.Rule, fn func(*netlink.Rule) error) error {
	for _, r := range rules {
		if err := fn(r); err != nil {
			return fmt.Errorf("priority %d: %w", r.Priority, err)
		}
	}
	return nil
}

// --- nftables -------------------------------------------------------------

// applyNFT replaces our table wholesale, in one transaction.
//
// Delete-then-recreate rather than a rule-by-rule reconciliation, and the
// reason is that the batch is atomic: no half-built ruleset is ever visible, so
// there is no ordering problem to solve inside it and no chance of a state
// where some flows are classified by the old rules and some by the new. It also
// means a hand-edit inside our table is corrected on every apply, which is what
// bounds the observation gap documented on observeNFT.
//
// It never touches anything else. `nft flush ruleset` is banned outright
// (design.md §3.4) — it silently kills Docker, podman, libvirt and k8s
// networking — so this deletes and recreates one table, by name.
func (k LinuxKernel) applyNFT(d Desired) error {
	conn, err := nftables.New()
	if err != nil {
		return err
	}
	defer conn.CloseLasting()

	tables, err := conn.ListTablesOfFamily(nftables.TableFamilyINet)
	if err != nil {
		return err
	}
	for _, t := range tables {
		if t.Name == TableName {
			conn.DelTable(t)
			break
		}
	}

	if !d.Enabled {
		return conn.Flush()
	}

	table := conn.AddTable(&nftables.Table{
		Family: nftables.TableFamilyINet,
		Name:   TableName,
	})

	for _, name := range d.Table.Counters {
		conn.AddObject(&nftables.CounterObj{Table: table, Name: name})
	}

	policy := nftables.ChainPolicyAccept
	classify := conn.AddChain(&nftables.Chain{
		Name:     ClassifyChain,
		Table:    table,
		Type:     nftables.ChainTypeFilter,
		Hooknum:  nftables.ChainHookPrerouting,
		Priority: nftables.ChainPriorityMangle,
		Policy:   &policy,
	})

	// Rules are added in evaluation order, which is the order Desired.Lines
	// prints them in — so following a packet down `nft list ruleset` and
	// following it down `olr diff` are the same exercise.
	for _, e := range d.Table.Exits {
		conn.AddRule(&nftables.Rule{
			Table: table, Chain: classify,
			Exprs:    restoreExprs(e),
			UserData: comment(e.RestoreLine()),
		})
	}
	for _, s := range d.Table.Sources {
		conn.AddRule(&nftables.Rule{
			Table: table, Chain: classify,
			Exprs:    sourceExprs(s),
			UserData: comment(s.Line()),
		})
	}
	for _, e := range d.Table.Exits {
		conn.AddRule(&nftables.Rule{
			Table: table, Chain: classify,
			Exprs:    accountExprs(e),
			UserData: comment(e.AccountLine()),
		})
	}
	conn.AddRule(&nftables.Rule{
		Table: table, Chain: classify,
		Exprs:    unpolicedExprs(),
		UserData: comment("nft count-unpoliced"),
	})

	if len(d.Table.SNAT) > 0 {
		natPolicy := nftables.ChainPolicyAccept
		post := conn.AddChain(&nftables.Chain{
			Name:     PostroutingChain,
			Table:    table,
			Type:     nftables.ChainTypeNAT,
			Hooknum:  nftables.ChainHookPostrouting,
			Priority: nftables.ChainPriorityNATSource,
			Policy:   &natPolicy,
		})
		for _, s := range d.Table.SNAT {
			conn.AddRule(&nftables.Rule{
				Table: table, Chain: post,
				Exprs:    snatExprs(s),
				UserData: comment(s.Line()),
			})
		}
	}

	return conn.Flush()
}

// comment stores a line in nft's own comment format, so `nft list ruleset`
// prints it rather than showing an opaque blob.
func comment(s string) []byte {
	return userdata.AppendString(nil, userdata.TypeComment, s)
}

// markBytes and maskBytes are native-endian, because that is how the kernel
// holds a mark in a register. Addresses are the opposite — they are compared
// against wire bytes — which is the one endianness trap in this file.
func markBytes(v uint32) []byte { return binaryutil.NativeEndian.PutUint32(v) }

// setOurMarkBits builds `dst = (src & ~MarkMask) ^ value`.
//
// nftables' bitwise expression combines a register with constants, not with
// another register, so this is the only shape available — and it happens to be
// exactly the discipline §3.2 asks for. Clearing our byte and then XOR-ing a
// value that lives entirely inside it is an OR in every case that matters, and
// every bit outside the mask passes through untouched.
func setOurMarkBits(reg uint32, value uint32) *expr.Bitwise {
	return &expr.Bitwise{
		SourceRegister: reg,
		DestRegister:   reg,
		Len:            4,
		Mask:           markBytes(^MarkMask),
		Xor:            markBytes(value),
	}
}

// maskOurMarkBits builds `dst = src & MarkMask`, for the comparisons.
func maskOurMarkBits(reg uint32) *expr.Bitwise {
	return &expr.Bitwise{
		SourceRegister: reg,
		DestRegister:   reg,
		Len:            4,
		Mask:           markBytes(MarkMask),
		Xor:            markBytes(0),
	}
}

// restoreExprs: `ct mark & MarkMask == exit  →  meta mark set (mark & ~MarkMask) ^ exit`
//
// The first half of §3.4's flow stickiness. An established connection gets back
// the exit it started on, so editing an assignment does not move traffic that
// is already flowing — which is what keeps such an edit `reload` rather than
// `disruptive`.
func restoreExprs(e ExitRule) []expr.Any {
	return []expr.Any{
		&expr.Ct{Register: 1, Key: expr.CtKeyMARK},
		maskOurMarkBits(1),
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: markBytes(e.Mark)},
		&expr.Meta{Key: expr.MetaKeyMARK, Register: 1},
		setOurMarkBits(1, e.Mark),
		&expr.Meta{Key: expr.MetaKeyMARK, SourceRegister: true, Register: 1},
	}
}

// sourceExprs classifies a new flow by where it came from.
//
// Three guards, each earning its place:
//
//   - `meta mark & MarkMask == 0` skips anything the restore rules already
//     decided, so a running connection is never reclassified mid-flight.
//   - `fib daddr type != local` is what makes this **forward-only** (§3.5).
//     Traffic addressed to the router itself is not forwarded, so classifying it
//     would send the box's own replies out an exit. It is also, incidentally,
//     why a proxy running on this box cannot loop: its upstream connections are
//     locally generated and never meet this chain at all.
//   - the family check is required because this is an `inet` table, where an
//     IPv4 payload offset applied to an IPv6 packet reads whatever happens to be
//     at byte 12 of the header.
func sourceExprs(s SourceRule) []expr.Any {
	v6 := s.Prefix.Addr().Is6()
	proto := byte(unix.NFPROTO_IPV4)
	offset, length := uint32(12), uint32(4)
	if v6 {
		proto = unix.NFPROTO_IPV6
		offset, length = 8, 16
	}

	addr := s.Prefix.Masked().Addr().AsSlice()
	mask := net.CIDRMask(s.Prefix.Bits(), len(addr)*8)

	return []expr.Any{
		&expr.Meta{Key: expr.MetaKeyMARK, Register: 1},
		maskOurMarkBits(1),
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: markBytes(0)},

		&expr.Meta{Key: expr.MetaKeyNFPROTO, Register: 1},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{proto}},

		&expr.Fib{Register: 1, ResultADDRTYPE: true, FlagDADDR: true},
		&expr.Cmp{Op: expr.CmpOpNeq, Register: 1,
			Data: binaryutil.NativeEndian.PutUint32(unix.RTN_LOCAL)},

		&expr.Payload{
			DestRegister: 1,
			Base:         expr.PayloadBaseNetworkHeader,
			Offset:       offset,
			Len:          length,
		},
		&expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: length,
			Mask: mask, Xor: make([]byte, length)},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: addr},

		&expr.Meta{Key: expr.MetaKeyMARK, Register: 1},
		setOurMarkBits(1, s.Mark),
		&expr.Meta{Key: expr.MetaKeyMARK, SourceRegister: true, Register: 1},
	}
}

// accountExprs counts a packet against its exit and records the decision on the
// connection.
//
// Counting here rather than in the classify rule is what makes the number
// mean bytes rather than connections: the classify rule sees only the first
// packet of a flow, because the restore rule handles every one after it.
//
// The counter is a **named object**, not an anonymous inline counter, and §3.3
// is emphatic about the difference: re-rendering the chain zeroes an anonymous
// counter, so adding an exit would silently reset every other exit's totals.
func accountExprs(e ExitRule) []expr.Any {
	return []expr.Any{
		&expr.Meta{Key: expr.MetaKeyMARK, Register: 1},
		maskOurMarkBits(1),
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: markBytes(e.Mark)},
		&expr.Objref{Type: unix.NFT_OBJECT_COUNTER, Name: e.Counter},
		&expr.Ct{Register: 1, Key: expr.CtKeyMARK},
		setOurMarkBits(1, e.Mark),
		&expr.Ct{Register: 1, SourceRegister: true, Key: expr.CtKeyMARK},
	}
}

// unpolicedExprs counts what matched nothing.
//
// §7.3's residual rule: show what you cannot account for. It is what makes
// per-exit totals reconcile against the box total, and a number with a stated
// boundary beats a number nobody can trust.
func unpolicedExprs() []expr.Any {
	return []expr.Any{
		&expr.Meta{Key: expr.MetaKeyMARK, Register: 1},
		maskOurMarkBits(1),
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: markBytes(0)},
		&expr.Objref{Type: unix.NFT_OBJECT_COUNTER, Name: UnpolicedCounter},
	}
}

// snatExprs rewrites the source address of traffic bound for a next hop (§5.3).
func snatExprs(s SNATRule) []expr.Any {
	out := []expr.Any{
		&expr.Meta{Key: expr.MetaKeyMARK, Register: 1},
		maskOurMarkBits(1),
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: markBytes(s.Mark)},
	}
	if s.Dev != "" {
		out = append(out,
			&expr.Meta{Key: expr.MetaKeyOIFNAME, Register: 1},
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: nulTerminated(s.Dev)},
		)
	}
	return append(out, &expr.Masq{})
}

// nulTerminated is how the kernel compares an interface name: a fixed 16-byte
// buffer, NUL-padded. Comparing the bare string matches nothing.
func nulTerminated(s string) []byte {
	b := make([]byte, unix.IFNAMSIZ)
	copy(b, s)
	return b
}
