package devices

import (
	"bytes"
	"net/http"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// The module's REST surface (design.md §3.2 rule 2, §6.2).
//
// Thin, like internal/dhcp/http.go: every decision was already made and tested
// in config.go, validate.go, list.go and apply.go, and this file only translates
// between those and HTTP. A rule that appears here and nowhere else is in the
// wrong place, because the CLI would not get it.

// HTTP serves the module. Its fields are what the module needs from core
// (design.md §3.1 — modules call core, not the reverse).
type HTTP struct {
	Applier Applier

	// Lock is core's one global apply lock (§3.6). Writes take it; reads never
	// do.
	Lock *core.Lock

	// Events is where a change is announced so other clients re-read.
	Events *core.Events
}

// Handler returns the module's routes. Core mounts this with the /api/devices
// prefix stripped.
func (h HTTP) Handler() http.Handler {
	mux := http.NewServeMux()

	// Intent.
	mux.HandleFunc("GET /config", h.getConfig)
	mux.HandleFunc("PUT /config", h.putConfig)
	mux.HandleFunc("PATCH /config", h.patchConfig)

	// Dry run. A POST because it takes a body, not because it changes anything.
	mux.HandleFunc("POST /plan", h.postPlan)

	// The join. Observed half never stored, always stamped (§4.5).
	//
	// There is deliberately no /categories route: the vocabulary already
	// reaches every client through the published schema (schema.go), and a
	// second endpoint serving the same list would be the second source that
	// eventually disagrees with the first.
	mux.HandleFunc("GET /list", h.getList)

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
// Same RFC 7386 array semantics as internal/dhcp: a patch containing "devices"
// replaces the whole list rather than merging into it, which is why naming one
// device is a PUT of the new document rather than a PATCH of one entry.
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
// There are no Steps, unlike internal/dhcp: storing the document is a single
// atomic write, so there is no half-finished state to report. Saying that with
// an absent field rather than an empty array keeps the difference visible.
type applyResponse struct {
	Plan   planView        `json:"plan"`
	Config Config          `json:"config"`
	Error  *core.ErrorBody `json:"error,omitempty"`
}

func (h HTTP) apply(w http.ResponseWriter, r *http.Request, cfg Config) {
	// Validated before the lock is taken. Validation is pure (§5.3.1), so
	// holding the lock to do it would only make a bad request slow down a good
	// one, and a 422 is more useful than a plan that cannot be applied.
	if res := Validate(cfg); !res.OK() {
		core.WriteError(w, http.StatusUnprocessableEntity,
			"invalid devices configuration", problems(res.Errors)...)
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
		plan = buildPlan(current, cfg)
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

	// Only when something actually changed. Unlike an apply that touches the
	// system, a no-op store is genuinely a no-op, so announcing it would wake
	// every client to re-read identical bytes.
	if !plan.Empty {
		h.Events.Publish(core.Event{Type: core.EventApplied, Module: ModuleName})
	}

	core.WriteJSON(w, http.StatusOK, applyResponse{Plan: plan, Config: stored})
}

// --- dry run --------------------------------------------------------------

// postPlan answers "what would this do?" without doing it. An empty body plans
// the stored intent, which is the §5.4 drift check — and for this module it is
// always empty, because the document is the only thing it owns.
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

	if res := Validate(desired); !res.OK() {
		core.WriteError(w, http.StatusUnprocessableEntity,
			"invalid devices configuration", problems(res.Errors)...)
		return
	}

	core.WriteJSON(w, http.StatusOK, buildPlan(current, desired))
}

// --- the list -------------------------------------------------------------

func (h HTTP) getList(w http.ResponseWriter, r *http.Request) {
	now := time.Now()

	list, probs, err := h.Applier.List(r.Context())
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	resp := listResponse{
		Devices:  make([]deviceView, 0, len(list)),
		Problems: problems(probs),
		AsOf:     now,
	}
	for _, d := range list {
		resp.Devices = append(resp.Devices, viewDevice(d))
	}

	core.WriteJSON(w, http.StatusOK, resp)
}
