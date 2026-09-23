package link

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// HTTP is this module's REST surface.
//
// The same shape as every other module's: an applier, the global apply lock,
// and the event bus. Nothing here knows about the CLI, the UI or MCP — they are
// all clients of what this serves (design.md §1).
type HTTP struct {
	Applier Applier
	Lock    *core.Lock
	Events  *core.Events

	// Claims lists addresses other modules own, which removing an address by
	// hand must not take away — today the uplink's. A function rather than a
	// view, for the reason internal/dial reaches its publisher through one: the
	// uplink is `dial`'s, and link importing dial is an arrow design.md §4.1
	// does not draw. Nil outside olrd, which means nothing is claimed.
	Claims func() []Claim

	// HostFindings are what olr could not take from the distribution's own
	// network configuration for a network's members (internal/host), listed
	// with the interfaces' other findings. Nil outside olrd.
	HostFindings func() []core.Problem

	// Dependents re-applies the modules that read this one's networks: every
	// arrow out of `link` in design.md §4.1. Nil outside olrd, which follows
	// nothing.
	//
	// It exists because those modules do not read a network, they are *built
	// from* one — dnsmasq is told an interface name, and the egress masquerade
	// a set of subnets — so a network that moves leaves them addressing the
	// network it left. The supervisor will not repair that on its own and is
	// right not to (superviseEvery's file-rewrite case): it cannot tell a
	// half-finished apply from an operator's hand-edit. Here that ambiguity
	// does not exist. Somebody just asked for this change, so the modules that
	// carry it follow immediately rather than at whatever hour a person works
	// out why half the network is on the old interface.
	//
	// Inside the apply lock, and after the change rather than with it: §5.2 is
	// explicit that there is no cross-module atomicity, so this is a sequence
	// of independent applies whose steps are reported alongside this module's,
	// not a transaction. Each is a no-op unless the change actually reached it.
	//
	// A failed apply follows nothing. What the box has then is a state nobody
	// asked for, and §5.3.2's answer to that is to report which steps landed
	// and let a re-run finish the job — which takes the dependents along with
	// it, at the point there is something settled for them to follow.
	Dependents func(ctx context.Context) []core.Step
}

// Routes is the module's surface, declared as data so that it can be
// enumerated rather than only served (core.Route).
//
// Core mounts these with the /api/link prefix stripped.
func (h HTTP) Routes() []core.Route {
	return []core.Route{
		// Intent.
		{
			Method: "GET", Path: "/config", Tool: "show config",
			Summary: "Show which interfaces have been handed to this router and what networks they carry.",
			Handler: h.getConfig,
		},
		{
			Method: "PUT", Path: "/config",
			Summary:  "Replace the adopted interfaces and the networks on them.",
			Body:     core.BodyFull,
			Mutating: true,
			Handler:  h.putConfig,
		},
		{
			Method: "PATCH", Path: "/config",
			Summary:  "Change the adopted interfaces or networks and leave the rest alone.",
			Body:     core.BodyRelaxed,
			Mutating: true,
			Handler:  h.patchConfig,
		},

		// Dry run. A POST because it takes a body, not because it changes
		// anything.
		{
			Method: "POST", Path: "/plan", Tool: "show plan",
			Summary: "Show what adopting an interface or changing a network would do, without doing it.",
			Body:    core.BodyRelaxed,
			Handler: h.postPlan,
		},

		// The join. Observed half never stored, always stamped (§4.5).
		// One address, by hand. Not intent — nothing stored changes — so it is
		// its own verb rather than a field in the document, and it is the only
		// way to reach an address no network or uplink accounts for without a
		// shell. Applier.RemoveAddress has what it refuses.
		{
			Method: "DELETE", Path: "/interfaces/{name}/addresses/{address...}",
			Summary: "Take one IPv4 address off an adopted interface that no network owns — " +
				"typically one a removed network left behind.",
			Mutating: true,
			Handler:  h.deleteAddress,
		},

		{
			Method: "GET", Path: "/interfaces", Tool: "show interfaces",
			Summary: "List this machine's network interfaces and the networks configured on them: " +
				"addresses, whether they are up, and which have been handed to this router.",
			Handler: h.getInterfaces,
		},
	}
}

// Handler returns the module's routes.
func (h HTTP) Handler() http.Handler { return core.RouteTable(h.Routes()) }

// --- intent -----------------------------------------------------------------

func (h HTTP) getConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.Applier.Load()
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	core.WriteJSON(w, http.StatusOK, cfg)
}

// putConfig replaces the whole document.
func (h HTTP) putConfig(w http.ResponseWriter, r *http.Request) {
	data, err := core.ReadBody(w, r)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	cfg, err := UnmarshalConfig(data)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	opts, err := options(r)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.apply(w, r, cfg, opts)
}

// options reads the query parameters an apply or a plan accepts.
//
// `keep_addresses` is Options.KeepAddresses, and a query parameter rather than
// a field in the body because it is not intent: nothing about it is stored, and
// a document that carried it would say something about every later apply.
func options(r *http.Request) (Options, error) {
	var opts Options
	if v := r.URL.Query().Get("keep_addresses"); v != "" {
		keep, err := strconv.ParseBool(v)
		if err != nil {
			return opts, fmt.Errorf("keep_addresses=%q is not true or false", v)
		}
		opts.KeepAddresses = keep
	}
	return opts, nil
}

// patchConfig changes named fields and leaves the rest alone.
//
// RFC 7386 array semantics, as everywhere else: a patch containing "adopted"
// replaces the whole list rather than merging into it. So adopting one
// interface is read-modify-write by the client — which is what `olr adopt`
// does, and what the UI's switch does. Merging would be the more convenient
// spelling and the wrong one: there would then be no way to express a release
// at all.
func (h HTTP) patchConfig(w http.ResponseWriter, r *http.Request) {
	current, err := h.Applier.Load()
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	currentJSON, err := MarshalConfig(current)
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	patch, err := core.ReadBody(w, r)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(bytes.TrimSpace(patch)) == 0 {
		core.WriteError(w, http.StatusBadRequest, "empty patch body")
		return
	}

	merged, err := core.MergePatch(currentJSON, patch)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	cfg, err := UnmarshalConfig(merged)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	opts, err := options(r)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.apply(w, r, cfg, opts)
}

// applyResponse carries the plan alongside the stored result.
//
// Steps are here now, unlike internal/devices'. Storing the document is still a
// single atomic write, but a network change continues into the kernel, and that
// part can land halfway — one address added and the next refused. §5.2 says no
// rollback, which only works if what did happen is reported.
type applyResponse struct {
	Plan   planView        `json:"plan"`
	Config Config          `json:"config"`
	Steps  []Step          `json:"steps,omitempty"`
	Error  *core.ErrorBody `json:"error,omitempty"`
}

func (h HTTP) apply(w http.ResponseWriter, r *http.Request, cfg Config, opts Options) {
	// Validated before the lock is taken. The name and subnet rules are pure
	// (§5.3.1), so holding the lock for them would only make a bad request slow
	// down a good one. The observed half is read here too, which is a syscall
	// rather than a lock, and re-read under the lock by Apply.
	observed := h.Applier.observeQuietly()
	if res := Validate(cfg, observed); !res.OK() {
		core.WriteError(w, http.StatusUnprocessableEntity,
			"invalid link configuration", problems(res.Errors)...)
		return
	}

	var (
		result   ApplyResult
		applyErr error
	)
	if err := h.Lock.Do(r.Context(), func() error {
		result, applyErr = h.Applier.ApplyWith(r.Context(), cfg, opts)
		if applyErr == nil && !result.Plan.Empty && h.Dependents != nil {
			for _, s := range h.Dependents(r.Context()) {
				result.Steps = append(result.Steps, Step(s))
			}
		}
		return nil
	}); err != nil {
		core.WriteError(w, http.StatusServiceUnavailable,
			"timed out waiting for the apply lock: "+err.Error())
		return
	}

	plan := withLockoutWarning(result.Plan, r, observed)
	stored := cfg
	if applyErr != nil {
		core.WriteJSON(w, http.StatusInternalServerError, applyResponse{
			Plan:   plan,
			Config: stored,
			Steps:  result.Steps,
			Error:  &core.ErrorBody{Message: applyErr.Error()},
		})
		return
	}
	if loaded, err := h.Applier.Load(); err == nil {
		stored = loaded
	}

	// Only when something actually changed. A no-op apply touched neither the
	// document nor the box, so announcing it would wake every client to re-read
	// identical bytes.
	//
	// When it *did* change, the event matters more than it looks: adopting an
	// interface is what makes the DHCP form stop rejecting it, and creating a
	// network is what gives that form a subnet to validate against. A client
	// showing both has to re-read the one it did not just write.
	if !plan.Empty {
		h.Events.Publish(core.Event{Type: core.EventApplied, Module: ModuleName})
	}

	core.WriteJSON(w, http.StatusOK, applyResponse{Plan: plan, Config: stored, Steps: result.Steps})
}

// removeResponse is what removing an address by hand reports: the steps, and
// the error when one failed.
type removeResponse struct {
	Steps []Step          `json:"steps"`
	Error *core.ErrorBody `json:"error,omitempty"`
}

func (h HTTP) deleteAddress(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	prefix, err := netip.ParsePrefix(r.PathValue("address"))
	if err != nil || !prefix.Addr().Is4() {
		core.WriteError(w, http.StatusBadRequest, fmt.Sprintf(
			"%q is not an IPv4 address with its mask, like 172.16.1.1/24", r.PathValue("address")))
		return
	}

	// Refused rather than warned about, unlike a plan's lockout warning. A plan
	// is followed by a confirmation; this is one click, and what it would cost
	// is the only session that could put the address back.
	if local, ok := localAddr(r); ok && local == prefix.Addr() {
		core.WriteError(w, http.StatusConflict, fmt.Sprintf(
			"you are connected to this router at %s; removing it would cut this session off. "+
				"Connect through another of its addresses and remove it from there", local))
		return
	}

	var claims []Claim
	if h.Claims != nil {
		claims = h.Claims()
	}

	var (
		steps []Step
		opErr error
	)
	if err := h.Lock.Do(r.Context(), func() error {
		steps, opErr = h.Applier.RemoveAddress(r.Context(), name, prefix, claims)
		return nil
	}); err != nil {
		core.WriteError(w, http.StatusServiceUnavailable,
			"timed out waiting for the apply lock: "+err.Error())
		return
	}

	switch {
	case errors.Is(opErr, ErrNotPresent):
		core.WriteError(w, http.StatusNotFound, opErr.Error())
		return
	case errors.Is(opErr, ErrNotAdopted), errors.Is(opErr, ErrNetworkOwned), errors.Is(opErr, ErrClaimed):
		core.WriteError(w, http.StatusUnprocessableEntity, opErr.Error())
		return
	case opErr != nil:
		core.WriteJSON(w, http.StatusInternalServerError, removeResponse{
			Steps: steps, Error: &core.ErrorBody{Message: opErr.Error()},
		})
		return
	}

	h.Events.Publish(core.Event{Type: core.EventApplied, Module: ModuleName})
	core.WriteJSON(w, http.StatusOK, removeResponse{Steps: steps})
}

// withLockoutWarning adds a warning when the change moves the address the
// caller is talking to us over.
//
// This is §5.5's scenario in its original wording — "you are on your laptop, on
// the LAN, changing the LAN" — and §5.5's guard, the dead-man's switch that
// reverts a change nobody confirms, **is not built**. Until it is, this warning
// is the only thing between an operator and a box they can no longer reach, so
// it names the interface and the address rather than saying something general
// about disruption.
//
// It warns rather than refuses. Renumbering the LAN you are on is a legitimate
// thing to want and an operator with console access does it deliberately; the
// plan is where olr says what it is about to cost, not where it overrules them.
func withLockoutWarning(plan planView, r *http.Request, observed []Interface) planView {
	local, ok := localAddr(r)
	if !ok {
		return plan
	}
	for _, change := range plan.Changes {
		iface, isIface := strings.CutPrefix(change.Path, "interfaces[")
		if !isIface {
			continue
		}
		iface = strings.TrimSuffix(iface, "]")
		for _, o := range observed {
			if o.Name != iface {
				continue
			}
			for _, p := range o.Prefixes {
				if p.Addr() != local {
					continue
				}
				plan.Warnings = append(plan.Warnings, core.Problem{
					Path: change.Path,
					Message: fmt.Sprintf("you are connected to this router at %s, which is on %s — "+
						"applying this changes that interface's addressing and will drop your "+
						"session. Have console access ready, and expect to reconnect on the new "+
						"address.", local, iface),
				})
				return plan
			}
		}
	}
	return plan
}

// localAddr is the address on this box that the request arrived at.
//
// net/http puts the listener's local address in the connection context, which
// for TCP is the address the client actually reached us on. A unix socket has
// none — that is the `olr` CLI talking over /run, which cannot lock itself out
// of anything.
func localAddr(r *http.Request) (netip.Addr, bool) {
	local, _ := r.Context().Value(http.LocalAddrContextKey).(net.Addr)
	tcp, ok := local.(*net.TCPAddr)
	if !ok {
		return netip.Addr{}, false
	}
	addr, ok := netip.AddrFromSlice(tcp.IP)
	if !ok {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}

// --- dry run ----------------------------------------------------------------

// postPlan answers "what would this do?" without doing it.
//
// An empty body plans the stored intent against itself, which is the §5.4 drift
// check. That used to be trivially empty because the document was the only thing
// this module owned; it is not any more. A network whose address somebody
// removed by hand shows up here as work to do, which is exactly the point.
func (h HTTP) postPlan(w http.ResponseWriter, r *http.Request) {
	data, err := core.ReadBody(w, r)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	current, err := h.Applier.Load()
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	desired := current
	if len(bytes.TrimSpace(data)) > 0 {
		desired, err = UnmarshalConfig(data)
		if err != nil {
			core.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	observed := h.Applier.observeQuietly()
	if res := Validate(desired, observed); !res.OK() {
		core.WriteError(w, http.StatusUnprocessableEntity,
			"invalid link configuration", problems(res.Errors)...)
		return
	}

	opts, err := options(r)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	core.WriteJSON(w, http.StatusOK,
		withLockoutWarning(buildPlan(current, desired, observed, opts), r, observed))
}

// --- the list ---------------------------------------------------------------

func (h HTTP) getInterfaces(w http.ResponseWriter, r *http.Request) {
	now := time.Now()

	observed, err := h.Applier.Observe()
	if err != nil {
		// Fatal here, unlike a presence source failing in internal/devices. An
		// interface list without the kernel is a list of names with no
		// addresses and no state — there is nothing left worth rendering, so
		// saying so beats rendering it.
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	cfg, err := h.Applier.Load()
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	res := Validate(cfg, observed)
	byName := observedByName(observed)

	list := Join(cfg, observed)
	resp := listResponse{
		Interfaces: make([]interfaceView, 0, len(list)),
		Groups:     make([]groupView, 0, len(cfg.Groups)),
		Problems:   problems(append(res.Errors, res.Warnings...)),
		AsOf:       now,
	}
	if h.HostFindings != nil {
		resp.Problems = append(resp.Problems, h.HostFindings()...)
	}
	for _, info := range list {
		resp.Interfaces = append(resp.Interfaces, viewInterface(info, byName, cfg))
	}
	for _, g := range cfg.Groups {
		resp.Groups = append(resp.Groups, viewGroup(g, byName))
	}

	core.WriteJSON(w, http.StatusOK, resp)
}
