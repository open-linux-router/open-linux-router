package core

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// The distribution's own instances of the daemons olr drives, and what to do
// about one that is in the way.
//
// olr starts its own instances from its own units (internal/dhcp/render.go and
// internal/dns/render.go both say why), so the distro's are not upgrades of ours
// or ours of theirs — they are a second daemon competing for one port.
//
// This table used to live in internal/cli/enable.go, consulted once, by `olr
// enable`, and printed to a terminal. That was the wrong place twice over. It
// was invisible to the WebUI, where most operators now meet the problem; and it
// was read at a single instant, so a box that grew a conflict afterwards —
// `apt install dnsmasq` a week later, which is exactly how this was found — was
// never told again. Here it is one table, read on every status request by every
// surface.

// Blocker is a reason a module cannot do its job, in a form an operator can act
// on.
//
// Separate from Problem, which reports a field that fails validation. A blocker
// is about the box rather than the configuration: nothing the operator can type
// into olr fixes it, so the shape carries the command that does.
type Blocker struct {
	// Kind discriminates for clients that want to branch. One value today;
	// named rather than implied so adding a second is not a breaking change.
	Kind string `json:"kind"`

	// Unit is the systemd unit responsible. The actionable field — a pid can be
	// killed and returns at the next boot, a unit can be disabled.
	Unit string `json:"unit,omitempty"`

	// Summary is the one sentence a status line has room for.
	Summary string `json:"summary"`

	// Detail is why it matters, for a surface with room to say so.
	Detail string `json:"detail,omitempty"`

	// Fix is shell, verbatim and ready to paste. Multi-line where the correct
	// answer is, and commented where a line explains rather than runs.
	//
	// Still shown even where Action can perform it. A tool that reaches outside
	// its own scope has to say what it is about to do, in the words the operator
	// would have used themselves — and an operator who would rather type it
	// than press a button is not wrong.
	Fix string `json:"fix,omitempty"`

	// Action is olr doing it, when this is a blocker olr knows how to clear.
	//
	// Absent — and omitted from the JSON entirely — for one it does not, so a
	// blocker olr cannot fix serialises and renders exactly as it did before
	// this existed. That is the whole degradation story: an unrecognised
	// distribution, or an incumbent that is nobody's business but the
	// operator's, keeps the text-only advice.
	Action *Action `json:"action,omitempty"`
}

// BlockerDistroBackend is the Kind of every blocker this file produces.
const BlockerDistroBackend = "distro_backend"

// DistroBackend is one distribution unit and the ports it takes.
type DistroBackend struct {
	// Unit is the distribution's unit name.
	Unit string

	// Ports are the ports it binds, so a module can ask about its own rather
	// than reading a list that mostly concerns somebody else.
	Ports []int

	// Holds renders those ports the way the operator sees them.
	Holds string

	// Detail is why this particular incumbent matters.
	Detail string

	// Fix is the command that resolves it.
	Fix string

	// ActionID is the stable handle a client names to ask olr to do it, and
	// ActionLabel is the button. Spelled out here rather than derived from the
	// unit name because the id is API surface — a client stores it and sends it
	// back — and belongs next to the operator-facing words it goes with.
	ActionID    string
	ActionLabel string

	// ActionRuns is what the button will do, listed before it is pressed.
	ActionRuns []string

	// perform does it, and its absence is how an entry says olr must not.
	//
	// It takes the backend so that the unit name is read from the row rather
	// than repeated in a closure, which is the one thing here that could drift
	// into disabling a unit other than the one described above it.
	perform func(context.Context, DistroBackend) []Step
}

// DistroBackends is the table. Order is the order an operator meets them.
var DistroBackends = []DistroBackend{
	{
		Unit:  "dnsmasq.service",
		Ports: []int{67, 53},
		Holds: "UDP/67 and :53",
		Detail: "Debian's dnsmasq package ships this unit and it binds both ports as " +
			"soon as it is installed. olr runs its own dnsmasq from olr-dhcp.service, " +
			"with its own configuration, so this one is a second server rather than " +
			"the one olr drives.",
		Fix:         "sudo systemctl disable --now dnsmasq.service",
		ActionID:    "standdown:dnsmasq.service",
		ActionLabel: "Stop dnsmasq.service and keep it off",
		ActionRuns:  []string{standDownCommand("dnsmasq.service")},
		perform:     standDown,
	},
	{
		Unit:  "unbound.service",
		Ports: []int{53},
		Holds: ":53",
		Detail: "Debian enables its own unbound on install, listening on 127.0.0.1:53. " +
			"olr runs a separate instance and owns :53 through its relay.",
		Fix:         "sudo systemctl disable --now unbound.service",
		ActionID:    "standdown:unbound.service",
		ActionLabel: "Stop unbound.service and keep it off",
		ActionRuns:  []string{standDownCommand("unbound.service")},
		perform:     standDown,
	},
	{
		// Not disabled, ever: this box resolves through it, so stopping it takes
		// name resolution away from the machine the operator is typing on until
		// olr's relay is up — including from apt, and from olr's own upstream
		// lookups. Telling it to give up the socket while it keeps running is
		// the correct move and is not a thing anybody guesses.
		//
		// The drop-in rather than an edit of resolved.conf: it is removable in
		// one command, it cannot collide with settings the operator put in the
		// main file, and it does not turn every future `apt upgrade` into a
		// conffile prompt.
		Unit:  "systemd-resolved.service",
		Ports: []int{53},
		Holds: ":53",
		Detail: "This box resolves names through systemd-resolved, so it must keep " +
			"running — stopping it would leave this machine unable to look anything " +
			"up until olr's relay is serving. It can hand over the socket instead.",
		Fix: "sudo mkdir -p /etc/systemd/resolved.conf.d\n" +
			"printf '[Resolve]\\nDNSStubListener=no\\n' |\n" +
			"  sudo tee /etc/systemd/resolved.conf.d/10-olr-yield-53.conf\n" +
			"sudo systemctl restart systemd-resolved",
		ActionID: "yield:systemd-resolved.service",
		// The label is the contract: "keeps running" is the thing the operator
		// needs to believe before pressing it, because every other button on
		// this page stops something.
		ActionLabel: "Hand :53 to olr and keep resolving",
		ActionRuns: []string{
			"printf '[Resolve]\\nDNSStubListener=no\\n' > " + resolvedDropIn,
			"systemctl restart systemd-resolved.service",
		},
		perform: yieldStubListener,
	},
}

// resolvedDropIn is where systemd-resolved is told to give up the socket.
//
// A variable so a test can point it at a temporary directory, the same reason
// osReleasePath is one. olrd can write it because packaging/systemd/olrd.service
// names /etc/systemd in ReadWritePaths — see the comment there, which is the
// other half of this.
var resolvedDropIn = "/etc/systemd/resolved.conf.d/10-olr-yield-53.conf"

// standDown stops a shadow instance of a backend olr itself runs, and stops it
// coming back at the next boot.
//
// This is the design.md §3.4 exception, and the shape of it is the argument for
// it: the only units reachable from here are the ones in the table above, every
// one of which is a second copy of a daemon olr drives itself. An OS component
// is never stood down — systemd-resolved is in this table and gets
// yieldStubListener instead.
//
// Over D-Bus through core.Unit rather than by running systemctl, for the reason
// unit_linux.go gives: the mechanism already exists and a second one would be a
// second thing to keep correct. ActionRuns still says `systemctl disable --now`,
// because that is what the box will look like afterwards and the sentence the
// operator would have typed.
func standDown(ctx context.Context, b DistroBackend) []Step {
	unit, err := newUnit(b.Unit)
	if err != nil {
		return []Step{{Description: standDownCommand(b.Unit), Error: err.Error()}}
	}
	return runTasks(ctx,
		task{"stop " + b.Unit, unit.Stop},
		task{"disable " + b.Unit + ", so it does not come back at the next boot", unit.Disable},
	)
}

// standDownCommand is the sentence an operator would have typed to do what
// standDown does over D-Bus. Built once, so the table's ActionRuns, the
// install action's forecast and the step descriptions cannot word it three
// ways.
func standDownCommand(unit string) string {
	return fmt.Sprintf("systemctl disable --now %s", unit)
}

// yieldStubListener tells systemd-resolved to give up :53 while it keeps
// running, which is distro.go's Fix for it performed rather than printed.
func yieldStubListener(ctx context.Context, b DistroBackend) []Step {
	unit, err := newUnit(b.Unit)
	if err != nil {
		return []Step{{Description: "restart " + b.Unit, Error: err.Error()}}
	}
	return runTasks(ctx,
		task{"write " + resolvedDropIn, func(context.Context) error {
			return WriteFileAtomic(resolvedDropIn, []byte("[Resolve]\nDNSStubListener=no\n"), 0o644)
		}},
		task{"restart " + b.Unit + ", so it reads that and releases :53", unit.Restart},
	)
}

// Clearable reports whether olr will do anything about this incumbent on
// request, which is what a caller outside this package needs before offering.
func (b DistroBackend) Clearable() bool { return b.perform != nil }

// action is what olr will do about this incumbent, or nil where it will not.
func (b DistroBackend) action() *Action {
	if b.perform == nil {
		return nil
	}
	return &Action{
		ID:    b.ActionID,
		Label: b.ActionLabel,
		Runs:  b.ActionRuns,
		do:    func(ctx context.Context) []Step { return b.perform(ctx, b) },
	}
}

// newUnit is a variable only so the tests can answer for a box they do not
// have — the same reason procRoot is one in port_linux.go. Every other caller
// goes through NewUnit directly.
var newUnit = NewUnit

// takesPort reports whether this backend binds the given port.
func (b DistroBackend) takesPort(port int) bool {
	for _, p := range b.Ports {
		if p == port {
			return true
		}
	}
	return false
}

// DistroConflicts reports the distribution units competing for a port.
//
// Live means running *or* enabled at boot, and the second half is not padding:
// a unit that is stopped right now and enabled will be back after the next power
// cut, fighting olr for the socket at the least convenient moment and with
// nobody watching. That is worth saying while the box is calm.
//
// Best-effort and never an error. A box with no service manager — a developer
// laptop, a container — cannot have a distro unit in the way, and "we could not
// ask" must not be reported to an operator as a conflict (design.md §5.4).
func DistroConflicts(ctx context.Context, port int) []Blocker {
	return distroConflicts(ctx, func(b DistroBackend) bool { return b.takesPort(port) })
}

// DistroConflictsAll reports every distribution unit in the way, whatever port
// it takes.
//
// What `olr enable` asks, because it is not acting on behalf of one module: it
// is about to claim the box, and every one of these will be a conflict for
// somebody. Asking per port instead would query the same unit once for each
// port it happens to bind.
func DistroConflictsAll(ctx context.Context) []Blocker {
	return distroConflicts(ctx, func(DistroBackend) bool { return true })
}

func distroConflicts(ctx context.Context, keep func(DistroBackend) bool) []Blocker {
	var out []Blocker
	for _, b := range DistroBackends {
		if !keep(b) {
			continue
		}
		unit, err := newUnit(b.Unit)
		if err != nil {
			return nil // no service manager to ask; nothing to report
		}
		status, err := unit.Status(ctx)
		if err != nil || !status.Installed || (!status.Active && !status.Enabled) {
			continue
		}
		out = append(out, Blocker{
			Kind:    BlockerDistroBackend,
			Unit:    b.Unit,
			Summary: fmt.Sprintf("%s is %s, and holds %s.", b.Unit, describeLiveness(status), b.Holds),
			Detail:  b.Detail,
			Fix:     b.Fix,
			Action:  b.action(),
		})
	}
	return out
}

// WriteBlockersText prints what is standing in a module's way, if anything.
//
// Here rather than in each module's print.go so that `olr dhcp status` and
// `olr dns status` cannot drift into describing the same conflict differently —
// which matters more than usual for this list, since one dnsmasq appears in
// both.
func WriteBlockersText(w io.Writer, blockers []Blocker) {
	for _, b := range blockers {
		fmt.Fprintf(w, "\nin the way:     %s\n%s\n", b.Summary, b.Detail)
		if b.Fix != "" {
			fmt.Fprintf(w, "\n%s\n", IndentLines(b.Fix, "  "))
		}
	}
}

// WriteFixHint names the command that clears these, when any of them can be.
//
// Separate from WriteBlockersText and printed once at the end rather than under
// each blocker: `olr dns fix` clears all of them in one go, and repeating it
// under three panels would read as three different things to run.
func WriteFixHint(w io.Writer, module string, blockers []Blocker) {
	actionable := Actionable(blockers)
	if len(actionable) == 0 {
		return
	}
	fmt.Fprintf(w, "\nolr can do %s for you:\n\n  sudo olr %s fix\n",
		plural(len(actionable), "that", "all of that"), module)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// IndentLines prefixes every line, so a multi-line fix stays one block rather
// than spilling back to the margin halfway through.
func IndentLines(s, prefix string) string {
	if s == "" {
		return s
	}
	return prefix + strings.ReplaceAll(s, "\n", "\n"+prefix)
}

// describeLiveness says which half of "live" is true, because the two have
// different urgency: one is breaking things now, the other at the next boot.
func describeLiveness(s UnitStatus) string {
	switch {
	case s.Active && s.Enabled:
		return "running and enabled at boot"
	case s.Active:
		return "running"
	default:
		return "enabled at boot"
	}
}
