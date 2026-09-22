package dial

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"slices"
)

// The seam between deciding what the uplink should be and making the kernel
// agree.
//
// design.md §10 requires netlink to sit behind an interface so everything above
// it is unit-testable off Linux, and this is the second module to need that
// seam after internal/link — whose three files this one is modelled on closely
// enough that reading either teaches the other. Everything above this line
// works in values: Validate checks, buildPlan compares, Desired translates, and
// an implementation of Writer contains no policy.
//
// There is no `exec.Command` behind it and there never may be (design.md §3.6).

// ErrNoKernel is returned by a writer that cannot program the uplink.
//
// Not called ErrUnsupported, which internal/link's writer does call it: this
// package already has one of those, declared in bind_other.go for the sockets
// that can only be bound on Linux. Two build-tagged files in one package cannot
// both own that name, and "the uplink cannot be programmed here" and "a socket
// cannot be bound here" are different enough to want separate errors anyway.
var ErrNoKernel = errors.New("configuring an uplink needs a Linux kernel")

// Desired is the kernel state the uplink asks for.
//
// One interface, not a list, because there is one uplink — see Uplink for why
// that is a decision rather than a simplification.
type Desired struct {
	// Interface is the kernel name.
	Interface string

	// Address is the address olr wants on it, with its mask. Invalid means olr
	// wants none, which is the state the DHCP-client and PPPoE forms will be in
	// before they have discovered one.
	Address netip.Prefix

	// Gateway is the next hop for the default route in the main table. Invalid
	// means olr writes no route.
	Gateway netip.Addr

	// Up asks for the interface to be brought up. Never down: taking an uplink
	// down is not something any configuration here implies, and an operator who
	// wants that has `ip link` and means it.
	Up bool

	// Retire is the address olr wrote for the previous uplink and this one no
	// longer calls for, on RetireFrom — which is not necessarily Interface. It
	// comes off last, and only once everything above has landed. See Retiring
	// for when there is one.
	Retire     netip.Prefix
	RetireFrom string
}

// Empty reports whether there is nothing for a writer to do.
func (d Desired) Empty() bool {
	return d.Interface == "" ||
		(!d.Address.IsValid() && !d.Gateway.IsValid() && !d.Up && !d.Retire.IsValid())
}

// Retiring is the address a change of uplink leaves behind: the one olr wrote
// for the stored uplink, when the desired one puts a different address or the
// same address on a different interface.
//
// Only between two static uplinks. Handing the uplink back leaves the address
// and the route exactly where they are, and so does changing to an uplink with
// no static address — Config.RemoveUplink has the argument, and it is the same
// one: olr never recorded what the box had before, so taking the address away
// could only leave it with less. Moving the way out is different. The new
// address and route are in place before the old address comes off, and an
// address olr wrote and no longer claims is exactly the leftover an operator
// cannot tell apart from somebody else's: a second interface on the same
// subnet, answering for an address nothing routes to.
func Retiring(stored, desired Config) (string, netip.Prefix) {
	if !stored.Uplink.HasIPv4() || !desired.Uplink.HasIPv4() {
		return "", netip.Prefix{}
	}
	before, after := stored.Uplink, desired.Uplink
	if before.Interface == after.Interface && before.IPv4.Address == after.IPv4.Address {
		return "", netip.Prefix{}
	}
	return before.Interface, before.IPv4.Address
}

// Step is one kernel operation, reported whether or not it succeeded.
//
// Same shape as internal/link's and internal/gateway's, so a client's
// plan-and-apply rendering works against any module's answer without a second
// implementation.
type Step struct {
	Description string `json:"description"`
	Done        bool   `json:"done"`
	Error       string `json:"error,omitempty"`
}

// Writer programs the uplink.
//
// Apply returns steps *alongside* an error rather than instead of one. There is
// no rollback (design.md §5.2/§5.3.2): if the address lands and the route does
// not, the address stays and is reported, and re-running finishes the job. A
// revert here would itself be a route change, attempted at the moment the
// evidence says route changes are failing — and quite possibly while the
// operator's own session is arriving over the route being reverted.
type Writer interface {
	Apply(ctx context.Context, desired Desired) ([]Step, error)

	// Observe reads back what is actually on the box: the interface's IPv4
	// addresses and the next hop of the default route in the main table.
	//
	// Read per request and never stored (§4.5). This is what makes drift mean
	// something here in the same way it does in internal/link — somebody who
	// runs `ip route del default` by hand has to show up on the same surface a
	// hand-edited config file does.
	Observe(ctx context.Context, iface string) (Observed, error)
}

// Observed is what the kernel currently has, for one interface.
type Observed struct {
	// Present reports whether the interface exists at all.
	Present bool

	// Up is administrative state.
	Up bool

	// Addrs are the interface's IPv4 addresses, link-local excluded, matching
	// what internal/link publishes so the two cannot disagree about whether
	// 169.254.x.x counts.
	Addrs []netip.Prefix

	// Gateway is the next hop of the default route in the main table, whichever
	// interface it leaves by. Invalid means there is no default route.
	//
	// Deliberately not filtered to this interface: "there is a default route,
	// but it goes somewhere else" is the single most useful thing this read can
	// tell an operator whose uplink looks configured and does not carry
	// traffic, and filtering would turn it into "there is no default route".
	Gateway netip.Addr

	// GatewayDev is the interface that default route leaves by, empty when
	// there is none.
	GatewayDev string

	// GatewayState is whether Gateway answers on GatewayDev, read from the
	// kernel's neighbour table: GatewayAnswers, GatewaySilent, or empty when
	// nothing has tried to reach it yet.
	//
	// The half of "is the uplink working" a route cannot say. A default route
	// via a gateway on the wrong segment is written, matches the config, and
	// carries nothing — and until this was read, that box showed green.
	GatewayState string

	// GatewaySeenOn is another interface on which Gateway does answer, when
	// there is one. It is usually the answer to "which of my NICs faces the
	// modem", and cheaper to state than to leave the operator to guess.
	GatewaySeenOn string
}

// The states Observed.GatewayState can take.
const (
	GatewayAnswers = "answers"
	GatewaySilent  = "silent"
)

// DesiredFor builds the writer's input from stored intent.
func DesiredFor(c Config) Desired {
	if c.Uplink == nil {
		return Desired{}
	}
	d := Desired{Interface: c.Uplink.Interface, Up: true}
	if c.Uplink.IPv4 != nil {
		d.Address = c.Uplink.IPv4.Address
		d.Gateway = c.Uplink.IPv4.Gateway
	}
	return d
}

// UplinkPlan is the difference between what the kernel has and what the uplink
// asks for, computed as values so it can be shown before it is done.
type UplinkPlan struct {
	Interface string

	// Add is the address to put on the interface, invalid when it is already
	// there or there is none to add.
	Add netip.Prefix

	// Gateway is the default route to write, invalid when the main table
	// already has it.
	Gateway netip.Addr

	// ReplacesGateway is the next hop currently in the main table, when writing
	// Gateway would displace a different one. Invalid when there is no default
	// route to displace — the difference between *taking over* the way out and
	// *providing* one, which is the difference between a disruptive change and
	// a harmless one.
	ReplacesGateway netip.Addr

	// BringUp reports that the interface is down and the uplink needs it up.
	BringUp bool

	// Foreign are the other IPv4 addresses on the interface. Reported and never
	// removed — see the writer's contract below — so that a DHCP client also
	// acting on this NIC is *visible* rather than either invisible or deleted.
	Foreign []netip.Prefix
}

// Empty reports whether the kernel already agrees.
func (p UplinkPlan) Empty() bool {
	return !p.Add.IsValid() && !p.Gateway.IsValid() && !p.BringUp
}

// PlanUplink diffs the observed kernel against what the uplink asks for.
//
// # What olr takes ownership of, stated plainly
//
// **The interface's address and the default route in the main table**, and not
// the interface's other addresses. That second half is the deliberate
// difference from internal/link's PlanAddrs, which removes every v4 address a
// network does not call for. The argument there was that a member interface
// only becomes one through two deliberate acts, so nothing else is likely to be
// addressing it. The uplink is the exact opposite case: it is the interface a
// distribution's DHCP client is most likely to also be acting on, and stripping
// what that client put there is the failure this whole object exists to stop —
// an operator's box losing its way out because olr decided it knew better.
//
// So foreign addresses are reported in the plan and left alone by the writer.
// The asymmetry is the safe one, and it is the same one link.Desired.AddOnly
// argues for: too many addresses is a state an operator can see and fix from a
// shell they can still reach, too few is one that takes the shell away.
//
// The route is different, and is owned: `RouteReplace` rather than
// delete-then-add, because replacing the default route is the one operation
// that must not leave a window with none.
func PlanUplink(d Desired, obs Observed) UplinkPlan {
	plan := UplinkPlan{Interface: d.Interface}
	if d.Interface == "" {
		return plan
	}

	plan.BringUp = d.Up && obs.Present && !obs.Up

	if d.Address.IsValid() && !slices.Contains(obs.Addrs, d.Address) {
		plan.Add = d.Address
	}
	for _, have := range obs.Addrs {
		if have != d.Address {
			plan.Foreign = append(plan.Foreign, have)
		}
	}

	if d.Gateway.IsValid() && (obs.Gateway != d.Gateway || obs.GatewayDev != d.Interface) {
		plan.Gateway = d.Gateway
		if obs.Gateway.IsValid() {
			plan.ReplacesGateway = obs.Gateway
		}
	}
	return plan
}

// DescribeUplinkPlan renders a plan as the lines a diff view shows.
func DescribeUplinkPlan(p UplinkPlan) []string {
	var lines []string
	if p.Add.IsValid() {
		lines = append(lines, fmt.Sprintf("+ ip addr add %s dev %s", p.Add, p.Interface))
	}
	if p.BringUp {
		lines = append(lines, fmt.Sprintf("+ ip link set %s up", p.Interface))
	}
	if p.Gateway.IsValid() {
		lines = append(lines, fmt.Sprintf("+ ip route replace default via %s dev %s",
			p.Gateway, p.Interface))
	}
	return lines
}

// RecordingWriter is a Writer that records instead of acting, for tests and for
// the --root development mode where there is no kernel worth programming.
type RecordingWriter struct {
	// Applied is every Desired this writer was handed, in order.
	Applied []Desired

	// Steps is what the last Apply reported, so a test can assert on the order
	// operations were attempted in — which is the property that matters here:
	// address, then up, then the route.
	Steps []Step

	// State is what Observe answers with, keyed by interface.
	State map[string]Observed

	// Err makes every step fail, for the partial-apply paths.
	Err error
}

// Apply implements Writer.
func (w *RecordingWriter) Apply(_ context.Context, d Desired) ([]Step, error) {
	w.Applied = append(w.Applied, d)

	var steps []Step
	record := func(format string, args ...any) {
		step := Step{Description: fmt.Sprintf(format, args...), Done: w.Err == nil}
		if w.Err != nil {
			step.Error = w.Err.Error()
		}
		steps = append(steps, step)
	}
	if d.Address.IsValid() {
		record("add %s to %s", d.Address, d.Interface)
	}
	if d.Up {
		record("bring %s up", d.Interface)
	}
	if d.Gateway.IsValid() {
		record("route traffic out via %s on %s", d.Gateway, d.Interface)
	}
	if d.Retire.IsValid() {
		record("remove %s from %s", d.Retire, d.RetireFrom)
	}

	w.Steps = steps
	if w.Err != nil {
		return steps, fmt.Errorf("%d of %d uplink operations failed", len(steps), len(steps))
	}
	return steps, nil
}

// Observe implements Writer.
func (w *RecordingWriter) Observe(_ context.Context, iface string) (Observed, error) {
	obs, ok := w.State[iface]
	if !ok {
		return Observed{}, nil
	}
	obs.Present = true
	return obs, nil
}
