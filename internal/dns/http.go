package dns

import (
	"bytes"
	"net/http"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// The module's REST surface (design.md §3.2 rule 2, §6.2).
//
// It is deliberately thin. Every decision here was already made and tested in
// config.go, validate.go, plan.go and apply.go; this file translates between
// those and HTTP and does nothing else. If a rule appears in this file that is
// not in one of those, it is in the wrong place — the CLI would not get it.

// ModuleName is the path segment, config section and event label for this
// module.
const ModuleName = "dns"

// HTTP serves the module. Its fields are what the module needs from core
// (design.md §3.1 — modules call core, not the reverse).
type HTTP struct {
	// Applier owns the read and write paths onto the system.
	Applier Applier

	// Lock is core's one global apply lock (§3.6). Writes take it; reads
	// never do.
	Lock *core.Lock

	// Events is where an applied change is announced so the UI can re-read.
	Events *core.Events
}

// Handler returns the module's routes. Core mounts this with the /api/dns
// prefix stripped, so the patterns here do not repeat the module's own name.
func (h HTTP) Handler() http.Handler {
	mux := http.NewServeMux()

	// Intent.
	mux.HandleFunc("GET /config", h.getConfig)
	mux.HandleFunc("PUT /config", h.putConfig)
	mux.HandleFunc("PATCH /config", h.patchConfig)

	// Dry run. A POST because it takes a body, not because it changes
	// anything — this is the HTTP spelling of `olr --dry-run` (§5.1), and it
	// is what lets an agent propose a change for a human to review (§6.4).
	mux.HandleFunc("POST /plan", h.postPlan)

	// Observed. Never stored, never revisioned, always stamped (§4.5).
	//
	// /queries and /names are read through the relay's socket on every
	// request. They are the reason to own :53 at all — the resolver leg buys
	// nothing that can be seen without them.
	mux.HandleFunc("GET /status", h.getStatus)
	mux.HandleFunc("GET /queries", h.getQueries)
	mux.HandleFunc("GET /names", h.getNames)

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

// putConfig replaces the whole document. Validated against the full schema
// projection (§10).
func (h HTTP) putConfig(w http.ResponseWriter, r *http.Request) {
	data, err := core.ReadBody(w, r)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	// UnmarshalConfig rejects unknown fields. A mistyped key that silently did
	// nothing would be the worst outcome here: a blocklist that did not load
	// looks exactly like a blocklist with nothing on it.
	cfg, err := UnmarshalConfig(data)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.apply(w, r, cfg)
}

// patchConfig changes named fields and leaves the rest alone, which is what
// `olr set` and a UI toggle both need.
//
// Note the array semantics inherited from RFC 7386: a patch containing
// "policies" replaces the whole list rather than merging into it. That is why
// adding one blocked name is a PUT of the new document rather than a PATCH.
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
	// Strict again on the merged result, so an unknown key in the patch is
	// still caught rather than being smuggled in by the merge.
	cfg, err := UnmarshalConfig(merged)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.apply(w, r, cfg)
}

// applyResponse always carries the plan and the steps, successful or not.
//
// §5.3.2: there is no rollback, so a half-finished change stays half-finished
// and the honest thing is to say which steps landed. A bare error would leave
// the operator to guess, and re-running is the documented repair.
type applyResponse struct {
	Plan  planView        `json:"plan"`
	Steps []Step          `json:"steps,omitempty"`
	Error *core.ErrorBody `json:"error,omitempty"`
}

func (h HTTP) apply(w http.ResponseWriter, r *http.Request, cfg Config) {
	// Validated before the lock is taken. Validation is pure (§5.3.1), so
	// holding the lock to do it would only make a bad request slow down a good
	// one, and a 422 is more useful than a plan that cannot be applied.
	if res := Validate(cfg, h.Applier.Links); !res.OK() {
		core.WriteError(w, http.StatusUnprocessableEntity,
			"invalid dns configuration", problems(res.Errors)...)
		return
	}

	var (
		result   ApplyResult
		applyErr error
	)
	// applyErr is captured rather than returned so that a failure still yields
	// the partial result below. The lock only cares about serialising.
	if err := h.Lock.Do(r.Context(), func() error {
		result, applyErr = h.Applier.Apply(r.Context(), cfg)
		return nil
	}); err != nil {
		core.WriteError(w, http.StatusServiceUnavailable,
			"timed out waiting for the apply lock: "+err.Error())
		return
	}

	// Published whenever anything was attempted, including a partial failure —
	// especially then. Something on the box changed and every client's idea of
	// it is now stale.
	if len(result.Steps) > 0 {
		h.Events.Publish(core.Event{Type: core.EventApplied, Module: ModuleName})
	}

	resp := applyResponse{Plan: viewPlan(result.Plan), Steps: result.Steps}
	if applyErr != nil {
		resp.Error = &core.ErrorBody{Message: applyErr.Error()}
		core.WriteJSON(w, http.StatusInternalServerError, resp)
		return
	}
	core.WriteJSON(w, http.StatusOK, resp)
}

// --- dry run --------------------------------------------------------------

// postPlan answers "what would this do?" without doing it.
//
// An empty body plans the *stored* intent, which by §5.4 is exactly the drift
// check: plan unchanged intent against reality and see whether the diff is
// empty.
func (h HTTP) postPlan(w http.ResponseWriter, r *http.Request) {
	data, err := core.ReadBody(w, r)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	var cfg Config
	if len(bytes.TrimSpace(data)) == 0 {
		cfg, err = h.Applier.Load()
	} else {
		cfg, err = UnmarshalConfig(data)
	}
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	plan, err := h.Applier.Plan(r.Context(), cfg)
	if err != nil {
		core.WriteError(w, http.StatusUnprocessableEntity, err.Error(),
			problems(plan.Validation.Errors)...)
		return
	}
	core.WriteJSON(w, http.StatusOK, viewPlan(plan))
}

// --- observed -------------------------------------------------------------

// serviceView is one backend's liveness.
type serviceView struct {
	Unit string `json:"unit"`

	// Status is what systemd knows. Absent, with Error set, when the query
	// itself failed — which is normal on a developer box with no D-Bus and must
	// not take the rest of the answer down with it.
	Status *ServiceStatus `json:"status,omitempty"`
	Error  string         `json:"error,omitempty"`
}

type statusResponse struct {
	Enabled bool `json:"enabled"`

	// Services is both backends, always in the same order: the resolver that
	// does the work, then the relay that fronts it. Reporting them separately
	// is the point — "DNS is broken" has two very different causes and only one
	// of them is ours.
	Services []serviceView `json:"services"`

	// Drifted is the §5.4 answer: does the stored intent still describe what is
	// actually on the box?
	Drifted    bool      `json:"drifted"`
	Drift      *planView `json:"drift,omitempty"`
	DriftError string    `json:"drift_error,omitempty"`

	// Stats is what the relay has seen since it started. Absent, with
	// StatsError set, when the relay is not answering — which is itself the
	// most useful thing the reply can say.
	Stats      *statsView `json:"stats,omitempty"`
	StatsError string     `json:"stats_error,omitempty"`

	// AsOf stamps the whole reply. Every observed object carries its freshness
	// so no surface can imply one it does not have (§4.5).
	AsOf time.Time `json:"as_of"`
}

func (h HTTP) getStatus(w http.ResponseWriter, r *http.Request) {
	resp := statusResponse{AsOf: time.Now()}

	cfg, err := h.Applier.Load()
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Enabled = cfg.Enabled

	// Each part of "health" is reported independently and none can suppress the
	// others (§5.4 — drift and backend liveness are different questions, and
	// with two backends there are three).
	for _, unit := range h.Applier.Backend.Units() {
		view := serviceView{Unit: unit}
		svc, err := h.Applier.service(unit)
		if err != nil {
			view.Error = err.Error()
		} else if status, err := svc.Status(r.Context()); err != nil {
			view.Error = err.Error()
		} else {
			view.Status = &status
		}
		resp.Services = append(resp.Services, view)
	}

	if plan, err := h.Applier.Plan(r.Context(), cfg); err != nil {
		resp.DriftError = err.Error()
	} else {
		view := viewPlan(plan)
		resp.Drifted = !plan.Empty()
		resp.Drift = &view
	}

	if h.Applier.Observer != nil {
		if _, stats, err := h.Applier.Observer.Queries(r.Context()); err != nil {
			resp.StatsError = err.Error()
		} else {
			view := viewStatsAPI(stats)
			resp.Stats = &view
		}
	}

	core.WriteJSON(w, http.StatusOK, resp)
}

type queriesResponse struct {
	Queries []queryView `json:"queries"`
	Stats   *statsView  `json:"stats,omitempty"`
	AsOf    time.Time   `json:"as_of"`
}

func (h HTTP) getQueries(w http.ResponseWriter, r *http.Request) {
	if h.Applier.Observer == nil {
		core.WriteError(w, http.StatusServiceUnavailable, errNoObserver)
		return
	}

	queries, stats, err := h.Applier.Observer.Queries(r.Context())
	if err != nil {
		// A 503 and not a 500: the relay being down is a state of the system,
		// not a fault in olrd, and the difference tells the operator which
		// thing to go and look at.
		core.WriteError(w, http.StatusServiceUnavailable, err.Error())
		return
	}

	resp := queriesResponse{Queries: make([]queryView, 0, len(queries)), AsOf: time.Now()}
	for _, q := range queries {
		resp.Queries = append(resp.Queries, viewQuery(q))
	}
	view := viewStatsAPI(stats)
	resp.Stats = &view

	core.WriteJSON(w, http.StatusOK, resp)
}

type namesResponse struct {
	Names []nameView `json:"names"`
	Stats *statsView `json:"stats,omitempty"`
	AsOf  time.Time  `json:"as_of"`
}

func (h HTTP) getNames(w http.ResponseWriter, r *http.Request) {
	if h.Applier.Observer == nil {
		core.WriteError(w, http.StatusServiceUnavailable, errNoObserver)
		return
	}

	names, stats, err := h.Applier.Observer.Names(r.Context())
	if err != nil {
		core.WriteError(w, http.StatusServiceUnavailable, err.Error())
		return
	}

	resp := namesResponse{Names: make([]nameView, 0, len(names)), AsOf: time.Now()}
	for _, n := range names {
		resp.Names = append(resp.Names, viewName(n))
	}
	view := viewStatsAPI(stats)
	resp.Stats = &view

	core.WriteJSON(w, http.StatusOK, resp)
}

const errNoObserver = "this olrd was built without a window onto the relay, so there is nothing to read"
