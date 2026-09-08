package link

import (
	"bytes"
	"net/http"
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
			Summary: "Show which interfaces have been handed to this router.",
			Handler: h.getConfig,
		},
		{
			Method: "PUT", Path: "/config",
			Summary:  "Replace the whole set of adopted interfaces.",
			Body:     core.BodyFull,
			Mutating: true,
			Handler:  h.putConfig,
		},
		{
			Method: "PATCH", Path: "/config",
			Summary:  "Change the adopted interfaces and leave the rest alone.",
			Body:     core.BodyRelaxed,
			Mutating: true,
			Handler:  h.patchConfig,
		},

		// Dry run. A POST because it takes a body, not because it changes
		// anything.
		{
			Method: "POST", Path: "/plan", Tool: "show plan",
			Summary: "Show what adopting or releasing an interface would do without doing it.",
			Body:    core.BodyRelaxed,
			Handler: h.postPlan,
		},

		// The join. Observed half never stored, always stamped (§4.5).
		{
			Method: "GET", Path: "/interfaces", Tool: "show interfaces",
			Summary: "List this machine's network interfaces: their addresses, whether they are up, " +
				"and which of them have been handed to this router.",
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
// No Steps, for the reason internal/devices gives: storing the document is a
// single atomic write, so there is no half-finished state to report.
type applyResponse struct {
	Plan   planView        `json:"plan"`
	Config Config          `json:"config"`
	Error  *core.ErrorBody `json:"error,omitempty"`
}

func (h HTTP) apply(w http.ResponseWriter, r *http.Request, cfg Config) {
	// Validated before the lock is taken. The name rules are pure (§5.3.1), so
	// holding the lock for them would only make a bad request slow down a good
	// one. The observed half is read here too, which is a syscall rather than a
	// lock, and re-read under the lock by Save.
	observed := h.Applier.observeQuietly()
	if res := Validate(cfg, observed); !res.OK() {
		core.WriteError(w, http.StatusUnprocessableEntity,
			"invalid link configuration", problems(res.Errors)...)
		return
	}

	var (
		stored   Config
		plan     planView
		applyErr error
	)
	if err := h.Lock.Do(r.Context(), func() error {
		current, err := h.Applier.Load()
		if err != nil {
			applyErr = err
			return nil
		}
		plan = buildPlan(current, cfg, observed)
		stored, applyErr = h.Applier.Save(cfg)
		return nil
	}); err != nil {
		core.WriteError(w, http.StatusServiceUnavailable,
			"timed out waiting for the apply lock: "+err.Error())
		return
	}

	if applyErr != nil {
		core.WriteJSON(w, http.StatusInternalServerError, applyResponse{
			Plan:   plan,
			Config: stored,
			Error:  &core.ErrorBody{Message: applyErr.Error()},
		})
		return
	}

	// Only when something actually changed. A no-op store is genuinely a no-op
	// here — nothing on the box was touched either way — so announcing it would
	// wake every client to re-read identical bytes.
	//
	// When it *did* change, the event matters more than it looks: adopting an
	// interface is what makes the DHCP form stop rejecting it, so a client
	// showing both needs to re-read the one it did not just write.
	if !plan.Empty {
		h.Events.Publish(core.Event{Type: core.EventApplied, Module: ModuleName})
	}

	core.WriteJSON(w, http.StatusOK, applyResponse{Plan: plan, Config: stored})
}

// --- dry run ----------------------------------------------------------------

// postPlan answers "what would this do?" without doing it. An empty body plans
// the stored intent, which is the §5.4 drift check — and here it is always
// empty, because the document is the only thing this module owns.
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

	core.WriteJSON(w, http.StatusOK, buildPlan(current, desired, observed))
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
		Problems:   problems(append(res.Errors, res.Warnings...)),
		AsOf:       now,
	}
	for _, info := range list {
		resp.Interfaces = append(resp.Interfaces, viewInterface(info, byName))
	}

	core.WriteJSON(w, http.StatusOK, resp)
}
