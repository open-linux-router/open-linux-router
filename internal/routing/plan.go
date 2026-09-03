package routing

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Planning is deliberately pure and reads *observed* state rather than a cached
// copy of what we last wrote. That is what makes drift free (design.md §5.4):
// "have we drifted?" is just "plan against unchanged intent and see if the diff
// is empty", so there is no separate health machinery to keep in step.
//
// The unit of comparison here is the kernel itself, not a rendered file. That
// is a real difference from internal/dhcp and internal/dns, which compare bytes
// on disk and then ask systemd whether a backend is alive. This module has no
// backend: what it configures *is* the kernel, so a hand-run `ip rule del`
// shows up as drift the same way a hand-edited config file does in the others.

// Impact says what applying a change will cost, so a UI can warn instead of
// spinning (design.md §5.3.3). Same vocabulary as internal/dhcp's and
// internal/dns's, because an operator should not have to learn a second one per
// module — though one word means something different here, see ImpactRestart.
type Impact int

const (
	// ImpactNone changes nothing that is running.
	ImpactNone Impact = iota

	// ImpactReload changes the classifier without moving anybody's traffic.
	// Established connections keep their exit, because `ct mark` restore
	// re-applies the decision that was made when the flow started (§3.4) —
	// which is exactly why editing a rule is this and not the word below.
	ImpactReload

	// ImpactRestart is in the vocabulary and is never produced here. There is
	// no daemon to bounce: a change either leaves flows alone or moves them,
	// with nothing in between. It stays declared so the type round-trips
	// against the other modules' plans rather than failing to decode one.
	ImpactRestart

	// ImpactDisruptive will move traffic that is currently flowing, which
	// breaks every established connection using that path — and, in the case
	// this module has to be most careful about, may be the connection the
	// operator is issuing the change over.
	ImpactDisruptive
)

func (i Impact) String() string {
	switch i {
	case ImpactNone:
		return "none"
	case ImpactReload:
		return "reload"
	case ImpactRestart:
		return "restart"
	case ImpactDisruptive:
		return "disruptive"
	}
	return fmt.Sprintf("Impact(%d)", int(i))
}

// MarshalText makes Impact a JSON string.
func (i Impact) MarshalText() ([]byte, error) { return []byte(i.String()), nil }

// UnmarshalText parses that string back.
//
// The pair has to exist together: Impact is an int with a text encoding, so
// without this a plan can be sent but never read, and `olr routing` is a client
// of its own module's API.
func (i *Impact) UnmarshalText(text []byte) error {
	switch string(text) {
	case "none":
		*i = ImpactNone
	case "reload":
		*i = ImpactReload
	case "restart":
		*i = ImpactRestart
	case "disruptive":
		*i = ImpactDisruptive
	default:
		return fmt.Errorf("unknown impact %q (want none, reload, restart or disruptive)", text)
	}
	return nil
}

// ForeignRule is somebody else's RPDB entry, seen and reported rather than
// touched (§6, design.md §3.4's read-broadly-write-narrowly rule).
type ForeignRule struct {
	Priority int    `json:"priority"`
	Family   string `json:"family"`
	Table    int    `json:"table"`

	// Selector is the rule's match, rendered as `ip rule` would print it, so
	// the operator can find it in output they already know how to read.
	Selector string `json:"selector"`

	// HasDefault reports whether the table it selects carries a default route.
	// That is the whole test: a foreign rule pointing at a table with a default
	// route is a second owner of "where does traffic go", and two owners of one
	// decision surface is the failure §6 refuses.
	HasDefault bool `json:"has_default"`
}

// Observed is the actual state of the system, read fresh.
type Observed struct {
	// Known reports whether the kernel answered at all.
	//
	// The distinction is the same one internal/dhcp draws with ServiceKnown:
	// "we could not tell" and "there is nothing there" are different answers,
	// and treating the first as the second would make every box we cannot read
	// — a developer's laptop, a container with no CAP_NET_ADMIN — report
	// permanent drift and offer to fix it.
	Known bool

	// Lines is the live state in the same canonical form Desired produces for
	// its nftables rules, RPDB entries and routes, so comparing them is a
	// string comparison.
	Lines []string

	// Sysctls are the per-interface settings this module owns, by key.
	//
	// Kept out of Lines on purpose. Every other line describes an object we
	// created and would delete; a sysctl is a pre-existing kernel setting we
	// only ever move in one direction. Diffing them as lines would make a
	// setting somebody else had already put at 0 look like ours to remove, and
	// "remove" for a sysctl has no meaning we could act on.
	Sysctls map[string]string

	// Foreign lists RPDB entries outside our range that select a table with a
	// default route.
	Foreign []ForeignRule

	// AllSendRedirects is the value of net.ipv4.conf.all.send_redirects, or nil
	// when it could not be read. The kernel ORs it with the per-interface
	// value, so our per-interface zero does nothing while this is 1 — and
	// writing `all` ourselves would change behaviour on interfaces nobody
	// handed us (design.md §3.4). Reported, not written.
	AllSendRedirects *bool

	// Active is the set of source addresses currently sending traffic through
	// us, newest observation first.
	//
	// This is what makes `disruptive` a fact rather than a guess, the same move
	// internal/dhcp makes against the live lease database: the honest question
	// is not "did an assignment field change" but "is anybody using the path
	// that is about to move".
	Active []netip.Addr
}

// Plan is the full answer to "what would applying this config do?".
type Plan struct {
	Changes []Change `json:"changes"`
	Impact  Impact   `json:"impact"`

	// Foreign is carried on the plan rather than only on status, because it is
	// a refusal reason and the operator needs to see it next to what they were
	// trying to do.
	Foreign []ForeignRule `json:"foreign,omitempty"`

	// Reasons explains the impact in the operator's terms.
	Reasons []string `json:"reasons,omitempty"`

	// Blocked is set when the plan cannot proceed at all. §6: we detect and
	// refuse rather than racing another owner of the routing table, and rather
	// than rewriting a file we do not own.
	Blocked string `json:"blocked,omitempty"`

	// Validation carries warnings even on success.
	Validation Result `json:"-"`
}

// Change is one line's worth of pending work, in the canonical form.
type Change struct {
	Kind ChangeKind `json:"kind"`
	Line string     `json:"line"`
}

// ChangeKind is what happens to a line of kernel state.
type ChangeKind string

const (
	ChangeAdd    ChangeKind = "add"
	ChangeRemove ChangeKind = "remove"
)

// Empty reports whether applying would change nothing. This is the drift check
// (design.md §5.4).
func (p Plan) Empty() bool { return len(p.Changes) == 0 }

// Diff renders the change as a unified-style diff, using core's differ — the
// one `olr diff` is built on for every module.
//
// The header is ours rather than core's for the reason core/diff.go gives:
// only the module knows what a change to its own state costs, so the impact
// annotation is not core's to write.
func (p Plan) Diff(before, after []string) string {
	var b strings.Builder
	b.WriteString("--- kernel routing state\n")
	b.WriteString("+++ kernel routing state (" + p.Impact.String() + ")\n")
	for _, l := range core.LineDiff(joinLines(before), joinLines(after)) {
		b.WriteString(l)
		b.WriteString("\n")
	}
	return b.String()
}

func joinLines(lines []string) []byte {
	if len(lines) == 0 {
		return nil
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

// BuildPlan renders the desired state and diffs it against what the kernel has.
//
// It validates first and returns the error rather than planning against a
// config that cannot be applied — the whole value of validation is that it
// happens before anything is written (design.md §5.3.1).
func BuildPlan(c Config, links LinkView, health Health, obs Observed, admin netip.Addr) (Plan, Desired, error) {
	result := Validate(c, links)
	if err := result.Err(); err != nil {
		return Plan{Validation: result}, Desired{}, err
	}

	desired := Render(c, links, health)
	plan := Plan{Validation: result, Foreign: obs.Foreign}

	// §6. Structural, never a hardcoded priority: mihomo and sing-box both move
	// their numbers between versions, so a check that named one would pass on
	// the release that broke it. The test is "a rule we do not own, selecting a
	// table that carries a default route" — which is the definition of a second
	// owner of this decision, whatever priority it happens to sit at.
	//
	// Checked only when there is something to install. Tearing our own state
	// down never conflicts with anybody, and refusing to do so would leave an
	// operator unable to back out of the very situation the refusal is about.
	if len(obs.Foreign) > 0 && desired.Enabled {
		plan.Blocked = describeForeign(obs.Foreign)
	}

	if !obs.Known {
		// Nothing to compare against. Reporting "everything must be created"
		// would be a plan nobody can act on, and reporting "no change" would be
		// a lie; an empty plan plus the unknown flag on the response is the
		// honest pair.
		return plan, desired, nil
	}

	plan.Changes = diffLines(obs.Lines, desired.objectLines())
	plan.Changes = append(plan.Changes, sysctlChanges(desired, obs)...)
	plan.Impact, plan.Reasons = classify(c, plan, obs, desired, admin)

	return plan, desired, nil
}

// diffLines reduces two canonical states to the lines that differ.
//
// Set difference rather than an ordered diff, because these lines are kernel
// objects rather than a file: an `ip rule` at priority 8102 is the same object
// whether it is read back second or fifth, and treating a reordering as a
// change would make every plan on a busy box look non-empty.
func diffLines(before, after []string) []Change {
	have := make(map[string]int, len(before))
	for _, l := range before {
		have[l]++
	}
	want := make(map[string]int, len(after))
	for _, l := range after {
		want[l]++
	}

	var out []Change
	for _, l := range after {
		if want[l] > 0 && have[l] == 0 {
			out = append(out, Change{Kind: ChangeAdd, Line: l})
			want[l]--
			continue
		}
		if have[l] > 0 {
			have[l]--
			want[l]--
		}
	}
	for _, l := range before {
		if have[l] > 0 {
			out = append(out, Change{Kind: ChangeRemove, Line: l})
			have[l]--
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Line != out[j].Line {
			return out[i].Line < out[j].Line
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}

// sysctlChanges reports the settings that are not already where we want them.
//
// One-directional by construction: a value that already matches produces
// nothing, and a value we do not want is never touched. That is design.md
// §3.4's write-narrowly rule applied to the one namespace in this module where
// we are editing something the box already had rather than creating something
// of our own.
func sysctlChanges(d Desired, obs Observed) []Change {
	var out []Change
	for _, s := range d.Sysctls {
		if have, ok := obs.Sysctls[s.Key]; ok && have == s.Value {
			continue
		}
		out = append(out, Change{Kind: ChangeAdd, Line: fmt.Sprintf("sysctl %s = %s", s.Key, s.Value)})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Line < out[j].Line })
	return out
}

// classify reduces the plan to a single impact plus the reasons behind it.
//
// §5.3.3 insists `disruptive` be a fact rather than a guess, and the reason is
// pointed: a classification that cried wolf would train the operator to click
// through the one dialog that matters. So each rung below is tied to something
// observed, not to a field having changed.
func classify(c Config, plan Plan, obs Observed, desired Desired, admin netip.Addr) (Impact, []string) {
	if len(plan.Changes) == 0 {
		return ImpactNone, nil
	}

	impact := ImpactReload
	var reasons []string

	// A route or an RPDB rule moving is what re-decides an *established* flow's
	// path. A classify rule changing is not: `ct mark` restore means a flow
	// that started before the edit keeps the exit it started with, and only new
	// connections see the new rule (§3.4). That asymmetry is the whole reason
	// the save/restore pair is worth two lines.
	moved := movedPaths(plan.Changes)
	if len(moved) > 0 {
		if using := activeUsers(c, obs, desired); len(using) > 0 {
			impact = ImpactDisruptive
			reasons = append(reasons, describeActive(using))
		} else {
			reasons = append(reasons,
				"traffic will change path, but nothing is currently using the routes that move")
		}
	}

	// §5.1's last row, and the one failure this module can cause that the
	// operator cannot recover from by clicking again: locking themselves out by
	// routing their own address somewhere that cannot reach the router.
	if admin.IsValid() {
		if exit, iface, ok := adminAffected(c, desired, admin); ok {
			impact = ImpactDisruptive
			reasons = append(reasons, fmt.Sprintf(
				"you are connected from %s, which is on %s and would be routed via %q — "+
					"applying this could disconnect you from the router",
				admin, iface, exit))
		}
	}

	if !desired.Enabled {
		reasons = append(reasons,
			"routing policy will be removed and every network will use the box's normal path")
	}

	// Reported here rather than in Validate because it is a fact about the
	// running kernel, not about the configuration — and the sysctl we would
	// have to write to fix it belongs to interfaces nobody handed us.
	if obs.AllSendRedirects != nil && *obs.AllSendRedirects && hasSendRedirects(desired) {
		reasons = append(reasons,
			"net.ipv4.conf.all.send_redirects is 1, and the kernel combines it with the "+
				"per-interface setting, so clients may still be told to bypass this router. "+
				"Set it to 0 yourself — olr does not write machine-wide sysctls")
	}

	return impact, reasons
}

// movedPaths reports the changes that re-route established traffic: routes and
// RPDB rules, as against classify rules.
func movedPaths(changes []Change) []Change {
	var out []Change
	for _, c := range changes {
		if strings.HasPrefix(c.Line, "route ") || strings.HasPrefix(c.Line, "rule ") {
			out = append(out, c)
		}
	}
	return out
}

// activeUsers reports which observed source addresses sit on a network whose
// exit assignment is about to take effect or change.
func activeUsers(c Config, obs Observed, desired Desired) []netip.Addr {
	var out []netip.Addr
	for _, addr := range obs.Active {
		for _, s := range desired.Table.Sources {
			if s.Prefix.Contains(addr) {
				out = append(out, addr)
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Less(out[j]) })
	return out
}

func describeActive(addrs []netip.Addr) string {
	const show = 4
	names := make([]string, 0, len(addrs))
	for _, a := range addrs {
		names = append(names, a.String())
	}
	listed, suffix := names, ""
	if len(listed) > show {
		listed, suffix = listed[:show], fmt.Sprintf(" and %d more", len(names)-show)
	}
	verb := "device is"
	if len(addrs) > 1 {
		verb = "devices are"
	}
	return fmt.Sprintf("%d %s sending traffic through a path this changes, "+
		"so their open connections will break and have to be re-established: %s%s",
		len(addrs), verb, strings.Join(listed, ", "), suffix)
}

// adminAffected reports whether the caller's own address would be classified.
func adminAffected(c Config, desired Desired, admin netip.Addr) (exit, iface string, ok bool) {
	for _, s := range desired.Table.Sources {
		if s.Prefix.Contains(admin) {
			return s.Exit, s.Interface, true
		}
	}
	return "", "", false
}

func hasSendRedirects(d Desired) bool {
	for _, s := range d.Sysctls {
		if strings.HasSuffix(s.Key, ".send_redirects") {
			return true
		}
	}
	return false
}

// describeForeign is §6's refusal, in the doc's own words.
//
// It names the count, the priorities and the table rather than saying "a
// conflict was detected", because the operator has to go and find these in
// somebody else's configuration file, and a message that does not say where to
// look is a message that generates a support question.
func describeForeign(rules []ForeignRule) string {
	prios := make([]string, 0, len(rules))
	tables := map[int]bool{}
	for _, r := range rules {
		prios = append(prios, fmt.Sprint(r.Priority))
		tables[r.Table] = true
	}
	ids := make([]int, 0, len(tables))
	for t := range tables {
		ids = append(ids, t)
	}
	sort.Ints(ids)
	names := make([]string, 0, len(ids))
	for _, t := range ids {
		names = append(names, fmt.Sprint(t))
	}

	return fmt.Sprintf(
		"something else is managing routing on this box (%d foreign ip rule(s), priority %s, "+
			"table %s). olr cannot share the routing table with it. If this is mihomo or "+
			"sing-box, set auto-route: false and retry",
		len(rules), strings.Join(prios, ", "), strings.Join(names, ", "))
}
