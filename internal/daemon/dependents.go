package daemon

import (
	"context"
	"log/slog"
	"net/netip"

	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/dhcp"
	"github.com/open-linux-router/open-linux-router/internal/dns"
	"github.com/open-linux-router/open-linux-router/internal/gateway"
	"github.com/open-linux-router/open-linux-router/internal/gateway/nat"
)

// Following design.md §4.1's arrows after the module at the tail of one
// changes.
//
// The rule the arrows do not state, because it only shows up in a running box:
// a dependent that *reads* a fact needs nothing, while a dependent that is
// *built from* one has to be rebuilt when it changes. `dns` reads the uplink's
// resolvers live and is not here. dnsmasq is handed an interface name and the
// egress masquerade a set of subnets — both are programmed, both go stale, and
// neither module can notice on its own because nothing in its own intent
// changed. Moving a network to another interface and finding DHCP still bound
// to the old one is what that costs, and it looks exactly like olr ignoring
// the change.
//
// Kept in one file rather than spread across the mounts, so that the answer to
// "what follows a network" is a list somebody can read.

// follow defers to a cascade assigned later in daemon.go than the mount that
// references it, and tolerates one that never was.
//
// The nil case is not defensive programming: it is the shape of the wiring. A
// cascade is assigned before olrd serves anything, so a request cannot arrive
// first — but `link` and `dial` are also constructed by other callers (the
// conformance tests call Routes on a zero value), and a module that refused to
// apply because nothing follows it would be a strange thing to have built.
func follow(fn *func(context.Context) []core.Step) func(context.Context) []core.Step {
	return func(ctx context.Context) []core.Step {
		if *fn == nil {
			return nil
		}
		return (*fn)(ctx)
	}
}

// dependent is one module that follows another's change.
type dependent struct {
	// name is the module's, for the log line and the step.
	name string

	// apply re-applies stored intent and reports whether anything changed.
	//
	// Stored intent, never an argument: this module's configuration is not
	// what changed, and passing it one would make this a second writer of a
	// document that already has an owner (§4.2).
	apply func(context.Context) (bool, error)
}

// applyDependents runs each one and reports what it did.
//
// Failures do not stop the ones after them. These are independent applies
// under §5.2, not a transaction, and a dnsmasq that will not start is no
// reason to leave the masquerade pointing at the wrong interface as well.
//
// A dependent that changed nothing says nothing. That is the common case by
// far — most link changes reach no other module — and a step per module per
// apply would bury the one line that matters on the day one does.
func applyDependents(ctx context.Context, logger *slog.Logger, deps ...dependent) []core.Step {
	var steps []core.Step
	for _, d := range deps {
		changed, err := d.apply(ctx)
		switch {
		case err != nil:
			logger.Error("a module that follows this change could not be applied",
				"module", d.name, "error", err)
			steps = append(steps, core.Step{
				Description: "re-apply " + d.name + " for this change",
				Error:       err.Error(),
			})
		case changed:
			logger.Info("re-applied a module that follows this change", "module", d.name)
			steps = append(steps, core.Step{
				Description: "re-applied " + d.name + " for this change",
				Done:        true,
			})
		}
	}
	return steps
}

// dhcpDependent re-renders dnsmasq's configuration and restarts it when the
// network it serves moved or was renumbered.
func dhcpDependent(a dhcp.Applier) dependent {
	return dependent{name: dhcp.ModuleName, apply: func(ctx context.Context) (bool, error) {
		cfg, err := a.Load()
		if err != nil {
			return false, err
		}
		res, err := a.Apply(ctx, cfg)
		return !res.Plan.Empty(), err
	}}
}

// gatewayDependent re-programs the routing half, whose rules classify traffic
// by the source networks `link` owns.
//
// The admin address is the zero value for the reason startGateway passes it:
// there is no operator connection to keep open here, because the caller is
// olrd following somebody else's change rather than serving theirs.
func gatewayDependent(a gateway.Applier) dependent {
	return dependent{name: gateway.ModuleName, apply: func(ctx context.Context) (bool, error) {
		cfg, err := a.Load()
		if err != nil {
			return false, err
		}
		res, _, err := a.Apply(ctx, cfg, netip.Addr{})
		return !res.Plan.Empty(), err
	}}
}

// natDependent rebuilds the NAT table, which holds the egress masquerade: the
// uplink's interface name from `dial`, and the source subnets from `link`.
//
// Named for the package rather than the module, unlike the two above: both
// halves answer to `gateway`, and a step saying "re-applied gateway" twice
// would be a worse answer than one that says which table was rebuilt.
func natDependent(a nat.Applier) dependent {
	return dependent{name: gateway.ModuleName + " NAT", apply: func(ctx context.Context) (bool, error) {
		cfg, err := a.Load()
		if err != nil {
			return false, err
		}
		res, _, err := a.Apply(ctx, cfg)
		return !res.Plan.Empty(), err
	}}
}

// dnsDependent re-renders the names the relay answers for this box, after
// `ingress` published or withdrew one.
//
// A SIGHUP to the relay and nothing else: the file is reloadable and the
// resolver never reads it (internal/dns/published.go). A dns that is switched
// off is left alone — re-applying it here would be the way to start it.
func dnsDependent(a dns.Applier) dependent {
	return dependent{name: dns.ModuleName, apply: func(ctx context.Context) (bool, error) {
		cfg, err := a.Load()
		if err != nil {
			return false, err
		}
		if !cfg.Enabled {
			return false, nil
		}
		res, err := a.Apply(ctx, cfg)
		return !res.Plan.Empty(), err
	}}
}
