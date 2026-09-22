package nat

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// The module's REST surface (design.md §3.2 rule 2, §6.2).
//
// Thin, like the other modules': every decision was already made and tested in
// config.go, validate.go, render.go and plan.go, and this file only translates
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

// Routes is the module's surface, declared as data so that it can be enumerated
// rather than only served (core.Route).
//
// Core mounts these with the /api/gateway prefix stripped, alongside the
// routing half's own routes — see gateway.HTTP.Routes.
func (h HTTP) Routes() []core.Route {
	// The gate every mutating route on this module carries (§6.2): ask first
	// with dry_run, go ahead with a disruptive plan only with confirm. Copied
	// from internal/gateway, which is the module that established it.
	gate := []core.QueryParam{
		{
			Name: "dry_run", Type: "boolean",
			Summary: "Answer with the plan and change nothing.",
		},
		{
			Name: "confirm", Type: "boolean",
			Summary: "Go ahead even if the plan is disruptive. Without this a disruptive change is refused, and the plan is returned instead.",
		},
	}

	return []core.Route{
		// Intent, one forward at a time.
		//
		// There is no `/config` route here, and that is the whole shape of the
		// move: the forwards are a field of `gateway`'s config, so the whole-
		// section GET, PUT and PATCH are served by `gateway`'s own `/config`
		// and this package never sees them. What it keeps are the item routes,
		// which exist because of RFC 7386's one surprising rule — a merge patch
		// replaces an array wholesale rather than merging into it, so a PATCH
		// meaning to edit one forward would take the others with it.
		//
		// The deeper reason is that without them the *edit* happens in the
		// client: every caller loads the document, splices the list itself, and
		// sends the whole thing back. That is two requests where the lock only
		// covers the second, and it is a rule — Config.Upsert keeping a
		// forward's slot, and so its counter — reimplemented once per client.
		{
			Method: "GET", Path: "/forwards", Tool: "show forwards",
			Summary: "List the port forwards: what arrives from outside, and where it goes.",
			Handler: h.getForwards,
		},
		{
			Method: "PUT", Path: "/forwards/{name}",
			Summary:  "Create or replace one port forward, leaving every other one alone.",
			Query:    gate,
			Mutating: true,
			Handler:  h.putForward,
		},
		{
			Method: "DELETE", Path: "/forwards/{name}",
			Summary:  "Remove one port forward.",
			Query:    gate,
			Mutating: true,
			Handler:  h.deleteForward,
		},

		// Dry run. A POST because it takes a body, not because it changes
		// anything.
		//
		// Under /forwards rather than at /plan, because `gateway`'s own /plan
		// is the routing half's and the two answer different questions about
		// different tables. Merging them into one plan was considered and is
		// the thing §0's three-lifetimes table argues against: one verdict over
		// two tables hides which of them drifted.
		{
			Method: "POST", Path: "/forwards/plan", Tool: "show forwards plan",
			Summary: "Show what a change to port forwarding would do without doing it. " +
				"An empty body plans the stored configuration, which answers whether the kernel has drifted.",
			Body:    core.BodyRelaxed,
			Handler: h.postPlan,
		},

		// Re-apply stored intent without changing it. This is the repair path
		// design.md §5.3.2 asks for in place of rollback.
		{
			Method: "POST", Path: "/forwards/apply",
			Summary: "Re-program the port forwards from the stored configuration, changing no intent. " +
				"This is the repair path for a half-applied change or rules removed by hand.",
			Mutating: true,
			Handler:  h.postApply,
		},

		// Observed. Never stored, always stamped with as_of (§4.5).
		{
			Method: "GET", Path: "/forwards/status", Tool: "status forwards",
			Summary: "Show each forward's counter, what the kernel is actually holding, " +
				"whether it still matches the stored configuration, and whether anything else " +
				"on this box is filtering forwarded traffic.",
			Handler: h.getStatus,
		},
	}
}

// Handler returns the module's routes.
func (h HTTP) Handler() http.Handler { return core.RouteTable(h.Routes()) }

// --- intent ---------------------------------------------------------------

// forwardsBody is what POST /forwards/plan takes and GET /forwards answers
// with: a list under one key, so the shape can grow a sibling without every
// client having to special-case a bare array.
type forwardsBody struct {
	Forwards []Forward `json:"forwards"`
}

// getForwards lists the stored forwards.
//
// The list and not the Config: `Enabled` and `Egress` are `gateway`'s to
// publish, and a reply from this route carrying a copy of them would be a
// second place for the switch to be read from and disagree.
func (h HTTP) getForwards(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.Applier.Load()
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	forwards := cfg.Forwards
	if forwards == nil {
		forwards = []Forward{}
	}
	core.WriteJSON(w, http.StatusOK, forwardsBody{Forwards: forwards})
}

// mutate is every write on this module: load, edit, validate, plan, apply — all
// of it inside the one global apply lock (§3.6).
//
// Holding the lock across the *read* as well as the write is the point. The
// alternative, which is what a client doing GET-edit-PUT gets, leaves a window
// between the two halves in which another writer lands and is then silently
// overwritten. There is no revision to make the write conditional on, so the
// lock has to cover both halves or it covers nothing that matters.
func (h HTTP) mutate(w http.ResponseWriter, r *http.Request, edit func(*Config) error) {
	h.mutateWith(w, r, false, edit)
}

// mutateWith is mutate with the disruptive gate optionally already satisfied.
//
// forceConfirm is set by POST /apply alone. That route re-programs intent the
// operator stored earlier — and confirmed then, if it needed confirming — so
// there is no new decision to put to them. Gating it would mean a box whose
// rules somebody flushed needs an extra flag to be repaired, and the drifted box
// is the one that most needs repairing.
func (h HTTP) mutateWith(w http.ResponseWriter, r *http.Request, forceConfirm bool, edit func(*Config) error) {
	dryRun, err := boolParam(r, "dry_run")
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	confirm, err := boolParam(r, "confirm")
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	confirm = confirm || forceConfirm

	var (
		editErr  error
		invalid  Result
		failed   error
		obs      Observed
		plan     Plan
		desired  Desired
		result   ApplyResult
		stored   Config
		applyErr error
		held     bool
	)

	if err := h.Lock.Do(r.Context(), func() error {
		cfg, err := h.Applier.Load()
		if err != nil {
			failed = err
			return nil
		}

		if err := edit(&cfg); err != nil {
			editErr = err
			return nil
		}
		cfg.Normalize()

		if res := Validate(cfg, h.Applier.Links); !res.OK() {
			invalid = res
			return nil
		}

		if obs, err = h.Applier.Observe(r.Context()); err != nil {
			failed = err
			return nil
		}
		plan, desired, err = BuildPlan(cfg, h.Applier.Links, obs)
		if err != nil {
			failed = err
			return nil
		}

		// Two reasons to stop with the plan and write nothing. A dry run asked
		// the question without wanting the answer acted on; an unconfirmed
		// disruptive change is §5.3.3's one interruption — the operator is about
		// to break connections that are open right now, and that is the single
		// case worth a second round trip.
		if dryRun || (plan.Impact == ImpactDisruptive && !confirm) {
			held = true
			// Re-read rather than keeping the value from before the edit. The
			// edit mutates Forwards in place, and a Config copied off the top
			// shares that array — so the "this is what is still stored" half of
			// the answer would have quietly moved with it.
			stored, _ = h.Applier.Load()
			return nil
		}

		result, stored, applyErr = h.Applier.ApplyPlanned(r.Context(), cfg, plan, desired)
		return nil
	}); err != nil {
		core.WriteError(w, http.StatusServiceUnavailable,
			"timed out waiting for the apply lock: "+err.Error())
		return
	}

	switch {
	case failed != nil:
		core.WriteError(w, http.StatusInternalServerError, failed.Error())
		return
	case editErr != nil:
		core.WriteError(w, statusFor(editErr), editErr.Error())
		return
	case !invalid.OK():
		core.WriteError(w, http.StatusUnprocessableEntity,
			"invalid port forwarding configuration", problems(invalid.Errors)...)
		return
	}

	view := viewPlan(plan, obs, desired)

	// A dry run answers with the plan alone, which is the same shape POST /plan
	// gives — a caller that asked "what would this do?" gets one answer to that
	// question regardless of which route it asked down.
	if dryRun {
		core.WriteJSON(w, http.StatusOK, view)
		return
	}

	if held {
		core.WriteJSON(w, http.StatusConflict, applyResponse{
			Plan:   view,
			Config: stored,
			Error: &core.ErrorBody{Message: "this would break connections that are open now; " +
				"repeat the request with confirm=true to go ahead"},
		})
		return
	}

	if applyErr != nil {
		core.WriteJSON(w, http.StatusInternalServerError, applyResponse{
			Plan:   view,
			Steps:  result.Steps,
			Config: stored,
			Error:  &core.ErrorBody{Message: applyErr.Error()},
		})
		return
	}

	if !view.Empty {
		h.Events.Publish(core.Event{Type: core.EventApplied, Module: Module})
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

	// The body carries forwards and nothing else. `Enabled` and `Egress` come
	// from the stored config either way: they are `gateway`'s fields, and a
	// plan that let this route override them would be answering a question
	// about a configuration nobody could have written here.
	desiredCfg := current
	if len(bytes.TrimSpace(data)) > 0 {
		var body forwardsBody
		if err := json.Unmarshal(data, &body); err != nil {
			core.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		desiredCfg.Forwards = body.Forwards
		desiredCfg.Normalize()
	}

	if res := Validate(desiredCfg, h.Applier.Links); !res.OK() {
		core.WriteError(w, http.StatusUnprocessableEntity,
			"invalid port forwarding configuration", problems(res.Errors)...)
		return
	}

	obs, err := h.Applier.Observe(r.Context())
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	plan, desired, err := BuildPlan(desiredCfg, h.Applier.Links, obs)
	if err != nil {
		core.WriteError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	core.WriteJSON(w, http.StatusOK, viewPlan(plan, obs, desired))
}

// postApply re-applies stored intent unchanged. The edit is the identity: what
// gets programmed is whatever is on disk.
func (h HTTP) postApply(w http.ResponseWriter, r *http.Request) {
	h.mutateWith(w, r, true, func(*Config) error { return nil })
}

// --- one item at a time ----------------------------------------------------

// putForward adds a forward, replaces one, or renames one.
//
// Renaming keeps the slot, and so the counter, which is the whole reason
// Config.Rename exists rather than the caller doing a delete and an add: the
// number an operator is watching to decide whether the port works should survive
// them fixing a typo in its name.
func (h HTTP) putForward(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	var fwd Forward
	if err := core.DecodeJSON(w, r, &fwd); err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	// An omitted name means "the one in the path", so the common case does not
	// have to say it twice.
	if fwd.Name == "" {
		fwd.Name = name
	}

	h.mutate(w, r, func(cfg *Config) error {
		if fwd.Name != name {
			if _, ok := cfg.Find(name); !ok {
				return notFound(unknownForward(cfg, name))
			}
			if _, taken := cfg.Find(fwd.Name); taken {
				return badRequest(fmt.Errorf(
					"cannot rename %q to %q: there is already a forward called %q",
					name, fwd.Name, fwd.Name))
			}
			// Rename first, so the slot follows; Upsert then applies the rest of
			// the body.
			cfg.Rename(name, fwd.Name)
		}
		cfg.Upsert(fwd)
		return nil
	})
}

func (h HTTP) deleteForward(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	h.mutate(w, r, func(cfg *Config) error {
		if !cfg.Remove(name) {
			return notFound(unknownForward(cfg, name))
		}
		return nil
	})
}

// --- observed -------------------------------------------------------------

func (h HTTP) getStatus(w http.ResponseWriter, r *http.Request) {
	now := time.Now()

	st, err := h.Applier.GetStatus(r.Context())
	if err != nil {
		// Degraded rather than fatal, the way internal/dhcp's getStatus reports
		// each half of its answer independently: a kernel we could not read
		// still leaves the configured forwards worth showing, and Known:false
		// says which half is missing.
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

// --- plumbing --------------------------------------------------------------

// editError is a refusal from an edit that knows its own HTTP status.
//
// Without it every edit failure would come back as one status, and a DELETE
// naming a forward that is not there would read the same as a malformed patch
// body. Those are different mistakes and the caller fixes them differently.
type editError struct {
	status int
	err    error
}

func (e editError) Error() string { return e.err.Error() }
func (e editError) Unwrap() error { return e.err }

// notFound marks an edit that named something the configuration does not have.
func notFound(err error) error { return editError{http.StatusNotFound, err} }

// badRequest marks an edit refused because the request itself was malformed,
// rather than because the configuration it would produce is invalid.
func badRequest(err error) error { return editError{http.StatusBadRequest, err} }

// statusFor is the status an edit's refusal deserves. Anything that did not say
// is treated as a config the caller could have got right — 422, matching the
// answer a failed Validate gives.
func statusFor(err error) int {
	var e editError
	if errors.As(err, &e) {
		return e.status
	}
	return http.StatusUnprocessableEntity
}

// boolParam reads a flag-style query parameter.
//
// Bare presence — `?confirm` — is true, because that is how a flag reads in a
// URL and a caller who wrote it meant it. A malformed value is refused rather
// than ignored: a client that meant to confirm a disruptive change and mistyped
// is much better off hearing about it than having the change quietly held or,
// worse, quietly made.
func boolParam(r *http.Request, name string) (bool, error) {
	q := r.URL.Query()
	if !q.Has(name) {
		return false, nil
	}
	raw := q.Get(name)
	if raw == "" {
		return true, nil
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s must be true or false, not %q", name, raw)
	}
	return v, nil
}
