package link

import (
	"bytes"
	"fmt"
	"net"
	"net/http"
	"net/netip"
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
	h.apply(w, r, cfg)
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
	h.apply(w, r, cfg)
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

func (h HTTP) apply(w http.ResponseWriter, r *http.Request, cfg Config) {
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
		result, applyErr = h.Applier.Apply(r.Context(), cfg)
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

	core.WriteJSON(w, http.StatusOK, withLockoutWarning(buildPlan(current, desired, observed), r, observed))
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
	for _, info := range list {
		resp.Interfaces = append(resp.Interfaces, viewInterface(info, byName, cfg))
	}
	for _, g := range cfg.Groups {
		resp.Groups = append(resp.Groups, viewGroup(g, byName))
	}

	core.WriteJSON(w, http.StatusOK, resp)
}
