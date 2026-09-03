package routing

import (
	"bytes"
	"net"
	"net/http"
	"net/netip"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// The module's REST surface (design.md §3.2 rule 2, §6.2).
//
// Thin, like internal/dhcp's and internal/dns's: every decision was already
// made and tested in config.go, validate.go, render.go and plan.go, and this
// file only translates between those and HTTP. A rule that appears here and
// nowhere else is in the wrong place, because the CLI would not get it.

// HTTP serves the module. Its fields are what the module needs from core
// (design.md §3.1 — modules call core, not the reverse).
type HTTP struct {
	Applier Applier

	// Lock is core's one global apply lock (§3.6). Writes take it; reads never
	// do.
	Lock *core.Lock

	// Events is where a change is announced so other clients re-read.
	Events *core.Events

	// Watch is called after a successful apply so the prober can follow the new
	// config. Nil is legal and means nothing is being probed.
	Watch func(Config)
}

// Handler returns the module's routes. Core mounts this with the /api/routing
// prefix stripped.
func (h HTTP) Handler() http.Handler {
	mux := http.NewServeMux()

	// Intent.
	mux.HandleFunc("GET /config", h.getConfig)
	mux.HandleFunc("PUT /config", h.putConfig)
	mux.HandleFunc("PATCH /config", h.patchConfig)

	// Dry run. A POST because it takes a body, not because it changes anything.
	mux.HandleFunc("POST /plan", h.postPlan)

	// Re-apply stored intent without changing it. This is the repair path
	// design.md §5.3.2 asks for in place of rollback: if an apply failed
	// halfway, or somebody ran `ip rule del` by hand, this finishes the job.
	mux.HandleFunc("POST /apply", h.postApply)

	// Observed. Never stored, always stamped with as_of (§4.5).
	mux.HandleFunc("GET /status", h.getStatus)

	return mux
}

// --- intent ---------------------------------------------------------------

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
// Same RFC 7386 array semantics as the other modules: a patch containing
// "exits" replaces the whole list rather than merging into it, which is why
// editing one exit is a PUT of the new document rather than a PATCH of one
// entry. Worth knowing here in particular, because a PATCH that dropped the
// other exits would take their traffic with them.
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

func (h HTTP) apply(w http.ResponseWriter, r *http.Request, cfg Config) {
	// Validated before the lock is taken. Validation is pure (§5.3.1), so
	// holding the lock to do it would only make a bad request slow down a good
	// one, and a 422 is more useful than a plan that cannot be applied.
	if res := Validate(cfg, h.Applier.Links); !res.OK() {
		core.WriteError(w, http.StatusUnprocessableEntity,
			"invalid routing configuration", problems(res.Errors)...)
		return
	}

	admin := callerAddr(r)

	var (
		result   ApplyResult
		stored   Config
		obs      Observed
		desired  Desired
		applyErr error
	)
	if err := h.Lock.Do(r.Context(), func() error {
		obs, _ = h.Applier.Observe(r.Context())
		result, stored, applyErr = h.Applier.Apply(r.Context(), cfg, admin)
		desired = Render(stored, h.Applier.Links, h.Applier.health())
		return nil
	}); err != nil {
		core.WriteError(w, http.StatusServiceUnavailable,
			"timed out waiting for the apply lock: "+err.Error())
		return
	}

	view := viewPlan(result.Plan, obs, desired)

	if applyErr != nil {
		// A refusal and a failure are different HTTP answers. §6's foreign-rule
		// refusal is a conflict the operator has to resolve elsewhere, not
		// something that went wrong here, and 409 is the word for that.
		status := http.StatusInternalServerError
		if result.Plan.Blocked != "" {
			status = http.StatusConflict
		}
		core.WriteJSON(w, status, applyResponse{
			Plan:   view,
			Steps:  result.Steps,
			Config: stored,
			Error:  &core.ErrorBody{Message: applyErr.Error()},
		})
		return
	}

	if h.Watch != nil {
		h.Watch(stored)
	}
	if !view.Empty {
		h.Events.Publish(core.Event{Type: core.EventApplied, Module: ModuleName})
	}

	core.WriteJSON(w, http.StatusOK, applyResponse{
		Plan: view, Steps: result.Steps, Config: stored,
	})
}

// --- dry run and repair ---------------------------------------------------

// postPlan answers "what would this do?" without doing it. An empty body plans
// the stored intent, which is the §5.4 drift check.
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

	desiredCfg := current
	if len(bytes.TrimSpace(data)) > 0 {
		desiredCfg, err = UnmarshalConfig(data)
		if err != nil {
			core.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	if res := Validate(desiredCfg, h.Applier.Links); !res.OK() {
		core.WriteError(w, http.StatusUnprocessableEntity,
			"invalid routing configuration", problems(res.Errors)...)
		return
	}

	obs, err := h.Applier.Observe(r.Context())
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	plan, desired, err := BuildPlan(desiredCfg, h.Applier.Links, h.Applier.health(), obs, callerAddr(r))
	if err != nil {
		core.WriteError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	core.WriteJSON(w, http.StatusOK, viewPlan(plan, obs, desired))
}

// postApply re-applies stored intent unchanged.
func (h HTTP) postApply(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.Applier.Load()
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.apply(w, r, cfg)
}

// --- observed -------------------------------------------------------------

func (h HTTP) getStatus(w http.ResponseWriter, r *http.Request) {
	now := time.Now()

	st, err := h.Applier.GetStatus(r.Context())
	if err != nil {
		// Degraded rather than fatal, the way internal/dhcp's getStatus reports
		// each half of its answer independently: a kernel we could not read
		// still leaves the configured exits worth showing, and Known:false says
		// which half is missing.
		core.WriteJSON(w, http.StatusOK, viewStatus(st, nil, now))
		return
	}

	cfg, err := h.Applier.Load()
	var warnings []Problem
	if err == nil {
		warnings = Validate(cfg, h.Applier.Links).Warnings
	}

	core.WriteJSON(w, http.StatusOK, viewStatus(st, warnings, now))
}

// callerAddr is the address the request came from, for §5.1's lockout check.
//
// The zero value for a caller over the unix socket, which is the common case
// for `olr` and is genuinely not at risk: a local caller cannot be disconnected
// by a routing change. Getting this wrong in the other direction would be worse
// than not having it — an invented address could match a network and make every
// apply from the socket look disruptive.
func callerAddr(r *http.Request) netip.Addr {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return netip.Addr{}
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	return addr.Unmap()
}
