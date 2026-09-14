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
	// Shown rather than performed, for now. When olr grows a button that yields
	// the port for the operator, this is the text it must display before acting:
	// a tool that reaches outside its own scope has to say what it is about to
	// do, in the words the operator would have used themselves.
	Fix string `json:"fix,omitempty"`
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
		Fix: "sudo systemctl disable --now dnsmasq.service",
	},
	{
		Unit:  "unbound.service",
		Ports: []int{53},
		Holds: ":53",
		Detail: "Debian enables its own unbound on install, listening on 127.0.0.1:53. " +
			"olr runs a separate instance and owns :53 through its relay.",
		Fix: "sudo systemctl disable --now unbound.service",
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
	},
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
