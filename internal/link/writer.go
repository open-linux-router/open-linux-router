package link

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strings"
)

// The seam between deciding what a network's addressing should be and making
// the kernel agree.
//
// design.md §10 requires netlink to sit behind an interface so everything above
// it is unit-testable off Linux, and this module needs that seam for the first
// time here: until groups landed, `link` wrote nothing that reached the system
// at all. Everything above this line works in values — Validate checks, BuildPlan
// compares, this translates — so an implementation of Writer contains no policy.
//
// There is no `exec.Command` behind it and there never may be (design.md §3.6).
// olrd.service's sandbox is nearly free precisely because the process spawns
// nothing, and the first shell-out silently costs all of it.

// ErrUnsupported is returned by a writer that cannot program addresses.
var ErrUnsupported = errors.New("configuring addresses needs a Linux kernel")

// Desired is the addressing one interface should end up with.
type Desired struct {
	// Interface is the kernel name.
	Interface string

	// Addrs are the IPv4 addresses olr wants on it, with masks. Empty means olr
	// wants none — which is how a member leaving a network is expressed.
	//
	// IPv4 only, and that is the scope decision rather than an oversight: v6
	// prefixes come from delegation and dnsmasq derives them from the interface
	// itself (`constructor:`), so olr has no v6 address to write. Observe reads
	// only v4 for the same reason — a writer that saw a SLAAC address and did
	// not want it would try to remove it.
	Addrs []netip.Prefix

	// Up asks for the interface to be brought up. Never down: taking an
	// interface down is not something any group configuration implies, and an
	// operator who wants that has `ip link` and means it.
	Up bool

	// AddOnly suppresses the removal half: addresses in Addrs are added if
	// missing, and a v4 address on the interface that Addrs does not call for
	// is left alone instead of being taken off.
	//
	// It exists for exactly one caller, Applier.Restore, and the distinction is
	// between two jobs that look alike and are not. An operator applying a
	// change has said what this interface's addressing *is*, and PlanAddrs'
	// ownership claim is what makes that mean something. Startup has been told
	// nothing; it is putting back what a reboot erased, and the box it is
	// putting it back on is one whose other address sources — a DHCP client on
	// a member interface, an operator's `ip addr add` — have not necessarily
	// finished running yet.
	//
	// Enforcing ownership against that is a race olr loses in the worst
	// possible way. The failure is concrete rather than theoretical: a
	// one-armed router — one NIC, serving the LAN it is also reached over —
	// has no uplink to move into `dial`, so its only address sits on a group
	// member, and a startup that removed it would take the box off the network
	// on every boot with the operator's only route back being physical.
	//
	// The asymmetry is the safe one. Too many addresses is a state an operator
	// can see and fix from a shell they can still reach; too few is one that
	// takes the shell away.
	AddOnly bool

	// Retire are addresses to take off if they are there, and nothing else.
	//
	// The precise opposite of the ownership claim above, and used where that
	// claim has ended: an interface that has left every network is no longer
	// olr's to address, so an apply removes exactly the router address olr put
	// there and leaves anything else on it alone. RetiredFor builds these, and
	// they always come with AddOnly set.
	Retire []netip.Prefix
}

// Step is one kernel operation, reported whether or not it succeeded.
//
// Same shape as internal/gateway's, so a client's plan-and-apply rendering works
// against any module's answer without a second implementation.
type Step struct {
	Description string `json:"description"`
	Done        bool   `json:"done"`
	Error       string `json:"error,omitempty"`
}

// Writer programs interface addressing.
//
// Apply returns steps *alongside* an error rather than instead of one. There is
// no rollback (design.md §5.2/§5.3.2): if a multi-interface change fails halfway
// the parts that landed stay landed and are reported, and re-running finishes
// the job. On this module a revert would itself be an address change with its
// own chance of failing — and the failure it would be attempting to recover
// from is quite likely "the operator just lost their connection to this box".
type Writer interface {
	Apply(ctx context.Context, desired []Desired) ([]Step, error)
}

// AddrPlan is the difference between what an interface has and what a group
// says it should have, computed as values so it can be shown before it is done.
type AddrPlan struct {
	Interface string

	// Add and Remove are the addresses to gain and lose.
	Add    []netip.Prefix
	Remove []netip.Prefix

	// BringUp reports that the interface is down and the network needs it up.
	BringUp bool
}

// Empty reports whether the kernel already agrees.
func (p AddrPlan) Empty() bool {
	return len(p.Add) == 0 && len(p.Remove) == 0 && !p.BringUp
}

// PlanAddrs diffs the observed interfaces against the networks that claim them.
//
// # What olr takes ownership of, stated plainly
//
// An interface that is a member of a group has its **IPv4 addressing owned
// entirely by olr**: any v4 address on it that the group does not call for is
// removed. That is a real claim and it is made deliberately, because the
// alternative is worse. To leave foreign addresses alone we would have to know
// which addresses are ours, which means tagging them at creation and trusting
// the tag — and a tag that survives a reboot, a `ip addr flush`, or somebody
// else's configuration management is not something netlink offers.
//
// The claim is bounded in the two ways that matter. An interface only becomes a
// member because somebody adopted it and then put it in a network, which is two
// deliberate acts. And WAN interfaces are `dial`'s and are never members: that
// sentence was vacuous until dial.Uplink existed, because there was nowhere
// else in olr to put an interface, so the uplink ended up in a network and this
// writer stripped the address the ISP had given it. There is somewhere now, and
// dial's validator refuses an uplink on an interface a network already carries
// — the two cannot both own one interface's addressing, and the refusal is what
// makes this paragraph true rather than aspirational.
//
// IPv6 is not touched at all. A SLAAC or delegated address on a member is left
// exactly where it is — see Desired.Addrs.
func PlanAddrs(c Config, observed []Interface) []AddrPlan {
	byName := make(map[string]Interface, len(observed))
	for _, iface := range observed {
		byName[iface.Name] = iface
	}

	var plans []AddrPlan
	for _, g := range c.Groups {
		for _, m := range g.Members {
			iface, present := byName[m]
			if !present {
				// Nothing to plan against an interface that does not exist.
				// Validate has already warned; inventing steps for it would
				// produce a plan whose every line fails.
				continue
			}

			plan := AddrPlan{Interface: m, BringUp: !iface.Up}

			var want []netip.Prefix
			if g.IPv4 != nil && g.IPv4.Subnet.IsValid() {
				want = append(want, g.IPv4.RouterPrefix())
			}

			have := ipv4Prefixes(iface.Prefixes)
			for _, w := range want {
				if !slices.Contains(have, w) {
					plan.Add = append(plan.Add, w)
				}
			}
			for _, h := range have {
				if !slices.Contains(want, h) {
					plan.Remove = append(plan.Remove, h)
				}
			}

			if !plan.Empty() {
				plans = append(plans, plan)
			}
		}
	}

	slices.SortFunc(plans, func(a, b AddrPlan) int { return strings.Compare(a.Interface, b.Interface) })
	return plans
}

// RetiredFor lists the router addresses a change leaves behind: for every
// interface that was a member of a network in before and is a member of none in
// after, the address that network had olr put on it.
//
// This is what `olr net rm` has always promised — "take its address off the
// interface" — and what an apply did not do, because DesiredFor walks the
// networks that exist and a removed one is not among them. The address stayed,
// with nothing left in olr that knew why: on the box it happened to, a second
// subnet answering on a NIC that no longer served one, and a card blaming the
// distribution for it.
//
// An interface still in some network is left out, because that network's own
// apply owns its addressing and removes what it does not call for anyway.
func RetiredFor(before, after Config) []Desired {
	member := map[string]bool{}
	for _, g := range after.Groups {
		for _, m := range g.Members {
			member[m] = true
		}
	}

	byIface := map[string][]netip.Prefix{}
	for _, g := range before.Groups {
		if g.IPv4 == nil || !g.IPv4.Subnet.IsValid() {
			continue
		}
		for _, m := range g.Members {
			if member[m] || slices.Contains(byIface[m], g.IPv4.RouterPrefix()) {
				continue
			}
			byIface[m] = append(byIface[m], g.IPv4.RouterPrefix())
		}
	}

	out := make([]Desired, 0, len(byIface))
	for iface, prefixes := range byIface {
		out = append(out, Desired{Interface: iface, AddOnly: true, Retire: prefixes})
	}
	slices.SortFunc(out, func(a, b Desired) int { return strings.Compare(a.Interface, b.Interface) })
	return out
}

// PlanRetire is RetiredFor against the kernel: only the addresses that are
// actually still there, so a plan does not promise to remove what is gone.
func PlanRetire(before, after Config, observed []Interface) []AddrPlan {
	byName := make(map[string]Interface, len(observed))
	for _, iface := range observed {
		byName[iface.Name] = iface
	}
	var plans []AddrPlan
	for _, d := range RetiredFor(before, after) {
		have := ipv4Prefixes(byName[d.Interface].Prefixes)
		plan := AddrPlan{Interface: d.Interface}
		for _, p := range d.Retire {
			if slices.Contains(have, p) {
				plan.Remove = append(plan.Remove, p)
			}
		}
		if !plan.Empty() {
			plans = append(plans, plan)
		}
	}
	return plans
}

// DesiredFor builds the writer's input from the stored networks.
func DesiredFor(c Config) []Desired {
	var out []Desired
	for _, g := range c.Groups {
		for _, m := range g.Members {
			d := Desired{Interface: m, Up: true}
			if g.IPv4 != nil && g.IPv4.Subnet.IsValid() {
				d.Addrs = append(d.Addrs, g.IPv4.RouterPrefix())
			}
			out = append(out, d)
		}
	}
	slices.SortFunc(out, func(a, b Desired) int { return strings.Compare(a.Interface, b.Interface) })
	return out
}

// ipv4Prefixes filters a list to the IPv4 entries.
//
// Interface.Prefixes has already dropped link-local, so 169.254.0.0/16 is not
// here to be mistaken for an address worth removing.
func ipv4Prefixes(in []netip.Prefix) []netip.Prefix {
	var out []netip.Prefix
	for _, p := range in {
		if p.Addr().Is4() {
			out = append(out, p)
		}
	}
	return out
}

// DescribeAddrPlan renders a plan as the lines a diff view shows.
func DescribeAddrPlan(p AddrPlan) []string {
	var lines []string
	if p.BringUp {
		lines = append(lines, fmt.Sprintf("+ ip link set %s up", p.Interface))
	}
	for _, a := range p.Add {
		lines = append(lines, fmt.Sprintf("+ ip addr add %s dev %s", a, p.Interface))
	}
	for _, a := range p.Remove {
		lines = append(lines, fmt.Sprintf("- ip addr del %s dev %s", a, p.Interface))
	}
	return lines
}

// RecordingWriter is a Writer that records instead of acting, for tests and for
// the --root development mode where there is no kernel worth programming.
type RecordingWriter struct {
	Applied []Desired
	Err     error
}

// Apply implements Writer.
func (w *RecordingWriter) Apply(_ context.Context, desired []Desired) ([]Step, error) {
	w.Applied = append(w.Applied, desired...)
	steps := make([]Step, 0, len(desired))
	for _, d := range desired {
		steps = append(steps, Step{
			Description: fmt.Sprintf("configure %s", d.Interface),
			Done:        w.Err == nil,
		})
	}
	return steps, w.Err
}
