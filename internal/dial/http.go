package dial

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// The module's REST surface (design.md §3.2 rule 2, §6.2).
//
// Deliberately thin: every decision here was made and tested in config.go,
// validate.go, view.go and apply.go. If a rule appears in this file that is not
// in one of those, it is in the wrong place — the CLI would not get it.

// HTTP serves the module.
type HTTP struct {
	// Applier owns the read and write paths onto the document.
	Applier Applier

	// Lock is core's one global apply lock (§3.6). Writes take it; reads never
	// do.
	Lock *core.Lock

	// Events is where an applied change is announced so the UI can re-read.
	Events *core.Events

	// Watch hands the stored config to the publisher after a write, and States
	// reads back what it knows. Both nil outside olrd — the conformance tests
	// call Routes on a zero value, and `status` then reports intent with no
	// observed half rather than refusing to answer.
	//
	// The publisher is reached through two function fields rather than held as
	// a dependency, exactly as internal/gateway reaches its prober: the
	// background loop belongs to the process, not to the module, and an Applier
	// that had to own one could not be constructed by a test.
	Watch  func(Config)
	States func() map[string]RecordState
}

// Routes is the module's surface, declared as data so it can be enumerated
// rather than only served. Core mounts these with the /api/dial prefix
// stripped.
func (h HTTP) Routes() []core.Route {
	return []core.Route{
		// Intent, whole section.
		{
			Method: "GET", Path: "/config", Tool: "show config",
			Summary: "Show the public names this router keeps pointing at itself, and where each one reads its address from. " +
				"Credentials are never returned.",
			Handler: h.getConfig,
		},
		{
			Method: "PUT", Path: "/config",
			Summary:  "Replace the whole set of dynamic DNS records.",
			Body:     core.BodyFull,
			Mutating: true,
			Handler:  h.putConfig,
		},
		{
			Method: "PATCH", Path: "/config",
			Summary:  "Change the dynamic DNS configuration, leaving unmentioned fields alone.",
			Body:     core.BodyRelaxed,
			Mutating: true,
			Handler:  h.patchConfig,
		},

		// Intent, one record at a time.
		//
		// These exist for the reason internal/ingress states about its services:
		// RFC 7386 replaces an array wholesale rather than merging into it, so a
		// PATCH meaning to edit one record would take the others with it. Without
		// them the edit happens in the client — load, splice, send back — which
		// is two requests where the lock only covers the second.
		{
			Method: "PUT", Path: "/records/{name}",
			Summary:  "Add one name to keep current, or replace it, leaving every other one alone.",
			Mutating: true,
			Handler:  h.putRecord,
		},
		{
			Method: "DELETE", Path: "/records/{name}",
			Summary: "Stop keeping one name current. The record is left at the provider pointing at " +
				"the address last published; this only stops olr updating it.",
			Mutating: true,
			Handler:  h.deleteRecord,
		},

		// Dry run. A POST because it takes a body, not because it changes
		// anything.
		{
			Method: "POST", Path: "/plan", Tool: "show plan",
			Summary: "Show what a dynamic DNS change would do without doing it. " +
				"An empty body plans the stored configuration.",
			Body:    core.BodyRelaxed,
			Handler: h.postPlan,
		},

		// Observed. Never stored, always stamped with as_of (§4.5).
		{
			Method: "GET", Path: "/status", Tool: "status",
			Summary: "Show, for each published name, when its address was last read, what it was, " +
				"and whether the last attempt to publish it succeeded.",
			Handler: h.getStatus,
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
	// Redacted on the way out, always. This is the response a UI renders into a
	// form and the CLI prints to a terminal, and no caller needs the real value
	// back — the only thing that reads it is the publisher, which loads the
	// config itself.
	core.WriteJSON(w, http.StatusOK, cfg.Redacted())
}

func (h HTTP) putConfig(w http.ResponseWriter, r *http.Request) {
	data, err := core.ReadBody(w, r)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	next, err := UnmarshalConfig(data)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.mutate(w, r, func(cfg *Config) error {
		*cfg = preserveSecrets(next, *cfg)
		return nil
	})
}

func (h HTTP) patchConfig(w http.ResponseWriter, r *http.Request) {
	patch, err := core.ReadBody(w, r)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(bytes.TrimSpace(patch)) == 0 {
		core.WriteError(w, http.StatusBadRequest, "empty patch body")
		return
	}

	h.mutate(w, r, func(cfg *Config) error {
		currentJSON, err := MarshalConfig(*cfg)
		if err != nil {
			return err
		}
		merged, err := core.MergePatch(currentJSON, patch)
		if err != nil {
			return badRequest(err)
		}
		// Strict again on the merged result, so an unknown key in the patch is
		// caught rather than smuggled in by the merge.
		next, err := UnmarshalConfig(merged)
		if err != nil {
			return badRequest(err)
		}
		*cfg = preserveSecrets(next, *cfg)
		return nil
	})
}

func (h HTTP) putRecord(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	var rec Record
	if err := core.DecodeJSON(w, r, &rec); err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	// The path wins over the body, and an omitted name means "the one in the
	// path" so the common case does not have to say it twice. Honouring the URL
	// is the reading that keeps PUT idempotent on the address it was sent to.
	rec.Name = name

	h.mutate(w, r, func(cfg *Config) error {
		if current, found := cfg.Find(name); found {
			rec = preserveRecordSecrets(rec, current)
		}
		cfg.SetRecord(rec)
		return nil
	})
}

func (h HTTP) deleteRecord(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	h.mutate(w, r, func(cfg *Config) error {
		if !cfg.RemoveRecord(name) {
			// 404, not 422. "There is no record by that name" is a statement
			// about the address, and a UI retrying a delete it already completed
			// should be able to tell that from a refusal.
			return notFound(fmt.Errorf("no record named %q", name))
		}
		return nil
	})
}

// preserveSecrets closes the round trip redaction creates.
//
// GET /config returns each credential as `********`. A UI that renders that into
// a form and sends the form back would otherwise *store the mask* — the
// publisher would authenticate with eight asterisks, every update would be
// refused from then on, and nothing about that is visible at the moment it
// happens. So the mask means "unchanged", which is the only thing it can
// honestly mean, and the real value is reachable by sending a different one.
//
// Matched by name, because that is the only identity a record has. A record
// being renamed and masked in one request therefore loses its credential, which
// is the correct reading: there is nothing to carry it over from.
func preserveSecrets(next, current Config) Config {
	for i := range next.Records {
		if have, found := current.Find(next.Records[i].Name); found {
			next.Records[i] = preserveRecordSecrets(next.Records[i], have)
		}
	}
	return next
}

func preserveRecordSecrets(next, current Record) Record {
	if next.Token == RedactedToken {
		next.Token = current.Token
	}
	if next.CallbackURL == RedactedToken {
		next.CallbackURL = current.CallbackURL
	}
	return next
}

// applyResponse carries the plan alongside the stored result.
//
// No Steps, for the reason internal/link gives: storing the document is a single
// atomic write, so there is no half-finished state to report.
type applyResponse struct {
	Plan   planView        `json:"plan"`
	Config Config          `json:"config"`
	Error  *core.ErrorBody `json:"error,omitempty"`
}

// mutate is the one write path every mutating route goes through.
//
// Load, edit, validate, plan and write, all inside the one global apply lock
// (§3.6). The lock has to cover both halves or it covers nothing that matters:
// two clients adding a record at once would otherwise each splice their own copy
// of the list.
//
// There is no disruptive gate here, unlike `ingress` and `gateway`. Nothing this
// module does drops a connection or takes a name away — removing a record leaves
// the name answering at the provider and merely stops it being refreshed — so
// §5.3.3's one interruption has nothing to interrupt. The consequence that *is*
// worth stating, that this box starts talking to a third party on a timer,
// reaches the operator as a plan warning, where they are already looking.
func (h HTTP) mutate(w http.ResponseWriter, r *http.Request, edit func(*Config) error) {
	var (
		editErr  error
		invalid  Result
		failed   error
		plan     planView
		stored   Config
		applyErr error
	)

	if lockErr := h.Lock.Do(r.Context(), func() error {
		current, err := h.Applier.Load()
		if err != nil {
			failed = err
			return nil
		}

		next := current.Clone()
		if err := edit(&next); err != nil {
			editErr = err
			return nil
		}
		next.Normalize()

		if res := Validate(next, h.Applier.Links); !res.OK() {
			invalid = res
			return nil
		}

		plan = buildPlan(current, next, h.Applier.Links)
		stored, applyErr = h.Applier.Save(next)
		return nil
	}); lockErr != nil {
		core.WriteError(w, http.StatusServiceUnavailable,
			"timed out waiting for the apply lock: "+lockErr.Error())
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
			"invalid dial configuration", problems(invalid.Errors)...)
		return
	}

	if applyErr != nil {
		core.WriteJSON(w, http.StatusInternalServerError, applyResponse{
			Plan:   plan,
			Config: stored.Redacted(),
			Error:  &core.ErrorBody{Message: applyErr.Error()},
		})
		return
	}

	// Only when something actually changed, and in this order: the publisher is
	// told before the event is published, so a UI that re-reads on the event
	// sees a status the publisher has already been handed rather than one it is
	// about to be.
	if !plan.Empty {
		if h.Watch != nil {
			h.Watch(stored)
		}
		h.Events.Publish(core.Event{Type: core.EventApplied, Module: ModuleName})
	}

	core.WriteJSON(w, http.StatusOK, applyResponse{Plan: plan, Config: stored.Redacted()})
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
		// The same round trip preserveSecrets closes, and it matters here too:
		// planning with the mask would report a credential change that a PUT of
		// the same body would not make.
		desired = preserveSecrets(desired, current)
	}

	if res := Validate(desired, h.Applier.Links); !res.OK() {
		core.WriteError(w, http.StatusUnprocessableEntity,
			"invalid dial configuration", problems(res.Errors)...)
		return
	}

	core.WriteJSON(w, http.StatusOK, buildPlan(current, desired, h.Applier.Links))
}

// --- observed ---------------------------------------------------------------

// getStatus is the three-questions surface (docs/ddns.md §6).
//
// It joins stored intent with what the publisher knows, per record. A record
// with no state has not been checked yet, which is a real transient right after
// a write and is reported as such rather than as a failure — the two look
// nothing alike to an operator and would otherwise be one blank column.
func (h HTTP) getStatus(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.Applier.Load()
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	states := map[string]RecordState{}
	if h.States != nil {
		states = h.States()
	}
	res := Validate(cfg, h.Applier.Links)
	byPath := map[string][]core.Problem{}
	for _, p := range append(problems(res.Errors), problems(res.Warnings)...) {
		name := recordNameFor(cfg, p.Path)
		byPath[name] = append(byPath[name], p)
	}

	resp := statusResponse{
		Records: make([]recordView, 0, len(cfg.Records)),
		AsOf:    time.Now(),
	}
	for _, rec := range cfg.Records {
		state, watched := states[rec.Name]
		view := recordView{
			Name:     rec.Name,
			Provider: rec.Provider.String(),
			Source:   rec.Source,
			From:     recordFrom(rec),
			Problems: byPath[rec.Name],
			Watched:  watched,
		}
		viewState(&view, state)
		resp.Records = append(resp.Records, view)
	}
	core.WriteJSON(w, http.StatusOK, resp)
}

// recordNameFor maps a validation path — `records[3].interface` — back to the
// record it is about, so a finding can be shown beside its own row.
func recordNameFor(c Config, path string) string {
	var index int
	if _, err := fmt.Sscanf(path, "records[%d]", &index); err != nil {
		return ""
	}
	if index < 0 || index >= len(c.Records) {
		return ""
	}
	return c.Records[index].Name
}

// --- helpers ----------------------------------------------------------------

// editError carries the status an edit failure should be reported with, so that
// "no such record" is a 404 and a malformed patch is a 400 without mutate having
// to know which edits can produce which.
type editError struct {
	status int
	err    error
}

func (e editError) Error() string { return e.err.Error() }
func (e editError) Unwrap() error { return e.err }

func notFound(err error) error   { return editError{status: http.StatusNotFound, err: err} }
func badRequest(err error) error { return editError{status: http.StatusBadRequest, err: err} }

func statusFor(err error) int {
	var e editError
	if errors.As(err, &e) {
		return e.status
	}
	return http.StatusUnprocessableEntity
}
