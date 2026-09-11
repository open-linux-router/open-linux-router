package firewall

import (
	"fmt"
	"sort"
	"strings"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Planning is deliberately pure and reads *observed* state rather than a cached
// copy of what we last wrote. That is what makes drift free (design.md §5.4):
// "have we drifted?" is just "plan against unchanged intent and see if the diff
// is empty", so there is no separate health machinery to keep in step.
//
// The unit of comparison is the kernel itself, not a rendered file. That is a
// real difference from internal/dhcp and internal/dns, which compare bytes on
// disk and then ask systemd whether a backend is alive. This module has no
// backend: what it configures *is* the kernel, so a hand-run `nft delete rule`
// shows up as drift the same way a hand-edited config file does in the others.

// Impact says what applying a change will cost, so a UI can warn instead of
// spinning (design.md §5.3.3). Same vocabulary as the other modules', because an
// operator should not have to learn a second one per module — though two words
// mean something specific here, see ImpactRestart and ImpactDisruptive.
type Impact int

const (
	// ImpactNone changes nothing that is running.
	ImpactNone Impact = iota

	// ImpactReload adds a translation without disturbing an existing one.
	//
	// Adding a forward is this: a connection that is already established was
	// never matched by the new rule, because DNAT applies to the first packet
	// of a connection and conntrack carries the translation for the rest. So a
	// new forward cannot move traffic that is already flowing.
	ImpactReload

	// ImpactRestart is in the vocabulary and is never produced here. There is
	// no daemon to bounce: a change either leaves connections alone or breaks
	// them, with nothing in between. It stays declared so the type round-trips
	// against the other modules' plans rather than failing to decode one.
	ImpactRestart

	// ImpactDisruptive will break connections that are currently translated.
	//
	// Removing or editing a forward is this, and for the reason above read
	// backwards: conntrack holds the translation for the life of the
	// connection, but the entry is only created while the rule exists. Take the
	// rule away and every established connection through it stops being
	// translated, so it dies mid-stream rather than being refused cleanly.
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
// without this a plan can be sent but never read, and `olr firewall` is a client
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

// ForeignFilter is somebody else's chain on the forward hook, seen and reported
// rather than touched (docs/firewall.md §5.2, design.md §3.4's
// read-broadly-write-narrowly rule).
//
// Only the ones that could stop a forwarded packet are collected: a chain whose
// policy is `accept` composes with ours harmlessly and saying so on every plan
// would be noise.
type ForeignFilter struct {
	// Table and Chain name where to go and look, because the operator has to
	// find this in somebody else's configuration and a message that does not
	// say where is a message that generates a support question.
	Table  string `json:"table"`
	Family string `json:"family"`
	Chain  string `json:"chain"`

	// Policy is the chain's default verdict, which is the thing that matters:
	// in nftables a drop is final, so an accept in our table cannot override a
	// drop in theirs.
	Policy string `json:"policy"`
}

// ListeningPort is something on this box already holding a port.
type ListeningPort struct {
	Protocol Protocol `json:"protocol"`
	Port     uint16   `json:"port"`
}

// Counter is one named counter object's totals.
//
// Both numbers, because they answer different questions. Packets is the one the
// planner uses — zero is proof that nothing has been translated, which is what
// keeps `disruptive` honest — while Bytes is what the operator reads on the
// status screen to tell a health check from a real session.
type Counter struct {
	Packets uint64 `json:"packets"`
	Bytes   uint64 `json:"bytes"`
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

	// Lines is the live state in the same canonical form Desired produces, so
	// comparing them is a string comparison.
	Lines []string

	// Counters is each named counter's totals, by object name.
	//
	// Read because it is what makes `disruptive` a fact rather than a guess
	// (§5.3.3): a forward whose counter has never moved is certainly not
	// carrying a connection, so removing it cannot break one. See classify.
	Counters map[string]Counter

	// Foreign lists chains on the forward hook, outside our table, whose policy
	// could stop a forwarded packet.
	Foreign []ForeignFilter

	// Listening is what this box is already serving on its own ports.
	//
	// The whole set rather than an answer about particular ports, because it is
	// small — tens of entries on a router — and because intersecting a set that
	// size with a forward's range is cheaper and more honest than asking about
	// 65535 ports one at a time.
	Listening []ListeningPort
}

// Plan is the full answer to "what would applying this config do?".
type Plan struct {
	Changes []Change `json:"changes"`
	Impact  Impact   `json:"impact"`

	// Foreign is carried on the plan rather than only on status, because it is
	// the reason a forward that looks right may not work, and the operator
	// needs to see it next to what they were trying to do.
	//
	// Unlike internal/gateway's ForeignRule this never blocks. docs/firewall.md
	// §5.2 has the argument: two owners of the routing table is a correctness
	// problem that silently misroutes, while a foreign forward-chain policy may
	// be exactly what the operator configured and may already accept this
	// traffic in a rule we cannot evaluate. Refusing would block a legitimate
	// setup on a guess.
	Foreign []ForeignFilter `json:"foreign,omitempty"`

	// Reasons explains the impact, and the warnings that are about the running
	// system rather than the configuration, in the operator's terms.
	Reasons []string `json:"reasons,omitempty"`

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

// Diff renders the change as a unified-style diff, using core's differ — the one
// `olr diff` is built on for every module.
//
// The header is ours rather than core's for the reason core/diff.go gives: only
// the module knows what a change to its own state costs, so the impact
// annotation is not core's to write.
func (p Plan) Diff(before, after []string) string {
	var b strings.Builder
	b.WriteString("--- kernel forwarding state\n")
	b.WriteString("+++ kernel forwarding state (" + p.Impact.String() + ")\n")
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
// It validates first and returns the error rather than planning against a config
// that cannot be applied — the whole value of validation is that it happens
// before anything is written (design.md §5.3.1).
func BuildPlan(c Config, links LinkView, obs Observed) (Plan, Desired, error) {
	result := Validate(c, links)
	if err := result.Err(); err != nil {
		return Plan{Validation: result}, Desired{}, err
	}

	desired := Render(c, links)
	plan := Plan{Validation: result}

	// Reported whenever there is something to install, and not when we are
	// tearing our own state down: a foreign filter cannot interfere with a
	// forward that is being removed, and saying so then would be noise attached
	// to the one operation it cannot affect.
	if desired.Enabled && len(desired.Table.DNAT) > 0 {
		plan.Foreign = obs.Foreign
	}

	if !obs.Known {
		// Nothing to compare against. Reporting "everything must be created"
		// would be a plan nobody can act on, and reporting "no change" would be
		// a lie; an empty plan plus the unknown flag on the response is the
		// honest pair.
		return plan, desired, nil
	}

	plan.Changes = diffLines(obs.Lines, desired.objectLines())
	plan.Impact, plan.Reasons = classify(c, plan, obs, desired)

	return plan, desired, nil
}

// diffLines reduces two canonical states to the lines that differ.
//
// Set difference rather than an ordered diff, because these lines are kernel
// objects rather than a file: a rule is the same object whether it is read back
// second or fifth, and treating a reordering as a change would make every plan
// on a busy box look non-empty.
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

// classify reduces the plan to a single impact plus the reasons behind it.
//
// §5.3.3 insists `disruptive` be a fact rather than a guess, and the reason is
// pointed: a classification that cried wolf would train the operator to click
// through the one dialog that matters.
func classify(c Config, plan Plan, obs Observed, desired Desired) (Impact, []string) {
	if len(plan.Changes) == 0 {
		return ImpactNone, nil
	}

	impact := ImpactReload
	var reasons []string

	// Only a *removal* can break something. An added rule cannot move a
	// connection that is already established, because DNAT applies to the first
	// packet of a connection and conntrack carries the translation for the rest.
	if removed := removedForwards(plan.Changes); len(removed) > 0 {
		if used := everUsed(removed, obs); len(used) > 0 {
			impact = ImpactDisruptive
			reasons = append(reasons, describeRemoved(used))
		} else {
			reasons = append(reasons,
				"a forward is being removed, but nothing has ever arrived through it, "+
					"so there is no connection to break")
		}
	}

	if !desired.Enabled {
		reasons = append(reasons,
			"port forwarding will be removed and nothing from outside will reach a device here")
	}

	// docs/firewall.md §5.2's second row. Reported here rather than in Validate
	// because it is a fact about the running box, not about the configuration:
	// the same document is correct on a machine where nothing holds the port.
	reasons = append(reasons, describeConflicts(c, obs)...)

	// And the first row. Same reasoning, and the same refusal to overstate:
	// this *may* stop the forward, because the foreign chain may have an accept
	// rule for it that we cannot evaluate.
	if len(plan.Foreign) > 0 {
		reasons = append(reasons, describeForeign(plan.Foreign))
	}

	return impact, reasons
}

// removedForwards reports the forwards named by the removals in a plan.
//
// Read off the canonical line rather than by diffing configs, because the line
// is what the kernel actually holds: a rule left behind by an older version, or
// one whose forward has already been deleted from the document, still names its
// forward and still carries connections.
func removedForwards(changes []Change) []string {
	seen := map[string]bool{}
	for _, c := range changes {
		if c.Kind != ChangeRemove {
			continue
		}
		if name, ok := forwardOfLine(c.Line); ok {
			seen[name] = true
		}
	}
	return sortedKeys(seen)
}

// forwardOfLine pulls the forward's name off a canonical rule line.
//
// Every rule line ends `... for <name>`, and a name cannot contain a control
// character (Validate) but may contain spaces — so the name is everything after
// the last " for ", not the last field.
func forwardOfLine(line string) (string, bool) {
	const sep = " for "
	i := strings.LastIndex(line, sep)
	if i < 0 {
		return "", false
	}
	name := line[i+len(sep):]
	if name == "" {
		return "", false
	}
	return name, true
}

// everUsed narrows a list of forwards to the ones a packet has actually arrived
// through.
//
// This is what keeps `disruptive` honest without a conntrack walk. The counter
// is monotonic since the table was built, so zero is a *proof* that nothing has
// been translated by this rule and therefore that no connection depends on it.
// A non-zero counter is weaker — it says traffic has been through, not that any
// is in flight right now — and the message says exactly that rather than
// claiming to know.
func everUsed(names []string, obs Observed) []string {
	if len(obs.Counters) == 0 {
		// No counters read at all: either the table is not there or we could not
		// see it. Treated as "cannot prove it is idle", which is the safe
		// direction — the operator gets one confirmation prompt rather than a
		// silently broken connection.
		return names
	}
	var out []string
	for _, name := range names {
		if obs.Counters[counterOfForward(name, obs)].Packets > 0 {
			out = append(out, name)
		}
	}
	return out
}

// counterOfForward finds the counter a forward's rules increment, by reading the
// live rule lines rather than by recomputing the slot.
//
// It has to be read rather than derived: the forward being removed is, by
// definition, the one whose slot the new configuration no longer knows.
func counterOfForward(name string, obs Observed) string {
	const marker = " counter "
	for _, line := range obs.Lines {
		if got, ok := forwardOfLine(line); !ok || got != name {
			continue
		}
		i := strings.Index(line, marker)
		if i < 0 {
			continue
		}
		rest := line[i+len(marker):]
		if j := strings.Index(rest, " "); j >= 0 {
			return rest[:j]
		}
	}
	return ""
}

func describeRemoved(names []string) string {
	quoted := make([]string, 0, len(names))
	for _, n := range names {
		quoted = append(quoted, fmt.Sprintf("%q", n))
	}
	verb := "forward has"
	if len(names) > 1 {
		verb = "forwards have"
	}
	return fmt.Sprintf(
		"%d %s carried traffic since this router started, so any connection still open "+
			"through them will stop working rather than being refused cleanly: %s",
		len(names), verb, strings.Join(quoted, ", "))
}

// describeConflicts is docs/firewall.md §5.2's local-port warning.
//
// The case that costs the operator their session is forwarding a port this box
// already serves on — SSH is the one that bites, because DNAT runs in prerouting
// before the local delivery decision, so the next connection from outside goes
// to the other machine and the person making the change is holding the
// connection it replaces.
//
// A warning rather than a refusal: "forward SSH to the NAS and administer this
// box from the LAN" is a legitimate thing to want. It is never a thing to do by
// accident.
func describeConflicts(c Config, obs Observed) []string {
	var out []string
	for _, f := range c.Forwards {
		var hit []string
		for _, l := range obs.Listening {
			if !f.Port.Valid() || l.Port < f.Port.From || l.Port > f.Port.To {
				continue
			}
			if !protocolCovers(f.ProtocolOrDefault(), l.Protocol) {
				continue
			}
			hit = append(hit, fmt.Sprintf("%s/%d", l.Protocol, l.Port))
		}
		if len(hit) == 0 {
			continue
		}
		sort.Strings(hit)
		out = append(out, fmt.Sprintf(
			"this router is itself listening on %s, and %q forwards that port onward. "+
				"The translation happens before the router decides to answer locally, so "+
				"connections from outside will reach %s instead of this box — including, if "+
				"this is the port you administer it over, yours",
			strings.Join(hit, ", "), f.Name, f.To.Addr()))
	}
	return out
}

func protocolCovers(want, got Protocol) bool {
	for _, p := range want.Each() {
		if p == got {
			return true
		}
	}
	return false
}

// describeForeign is docs/firewall.md §5.2's first row, in the doc's own words.
//
// It names the table and the chain rather than saying "a conflict was detected",
// because the operator has to go and find these in somebody else's
// configuration, and it is carefully phrased as *may* — the foreign chain may
// have an accept rule for this traffic that we cannot evaluate, and claiming
// otherwise would send somebody to debug a setup that works.
func describeForeign(filters []ForeignFilter) string {
	names := make([]string, 0, len(filters))
	for _, f := range filters {
		names = append(names, fmt.Sprintf("%s %s (chain %s, policy %s)",
			f.Family, f.Table, f.Chain, f.Policy))
	}
	sort.Strings(names)
	return fmt.Sprintf(
		"something else on this box is filtering forwarded traffic: %s. In nftables a drop is "+
			"final, so olr cannot override it — a forwarded connection may be stopped there even "+
			"though these rules are correct. The counter on each forward says whether packets are "+
			"arriving at all",
		strings.Join(names, ", "))
}
