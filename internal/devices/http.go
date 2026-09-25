package devices

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"strings"
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

// Routes is the module's surface, declared as data so that it can be
// enumerated rather than only served (core.Route).
//
// Core mounts these with the /api/devices prefix stripped.
func (h HTTP) Routes() []core.Route {
	return []core.Route{
		// Intent.
		{
			Method: "GET", Path: "/config", Tool: "show config",
			Summary: "Show the stored device inventory: names, categories and owners.",
			Handler: h.getConfig,
		},
		{
			Method: "PUT", Path: "/config",
			Summary:  "Replace the whole device inventory.",
			Body:     core.BodyFull,
			Mutating: true,
			Handler:  h.putConfig,
		},
		{
			Method: "PATCH", Path: "/config",
			Summary:  "Change named device fields and leave the rest alone.",
			Body:     core.BodyRelaxed,
			Mutating: true,
			Handler:  h.patchConfig,
		},

		// Intent, one item at a time, for the reason internal/gateway/http.go
		// gives: a merge patch replaces an array wholesale, so without these
		// every client would load the document, splice it and send it back —
		// two requests with the lock covering only the second, and the rename
		// cascade reimplemented once per client. Here each is one request under
		// the lock, calling config.go.
		{
			Method: "PUT", Path: "/groups/{name}",
			Summary:  "Create a device group, or rename one by sending a different name in the body.",
			Mutating: true,
			Handler:  h.putGroup,
		},
		{
			Method: "DELETE", Path: "/groups/{name}",
			Summary:  "Remove a device group; its devices become ungrouped.",
			Mutating: true,
			Handler:  h.deleteGroup,
		},
		{
			Method: "PUT", Path: "/devices/{mac}/group",
			Summary:  "Put one device in a group, or take it out of its group with an empty name.",
			Mutating: true,
			Handler:  h.putDeviceGroup,
		},

		// Dry run. A POST because it takes a body, not because it changes
		// anything.
		{
			Method: "POST", Path: "/plan", Tool: "show plan",
			Summary: "Show what a change to the device inventory would do without doing it.",
			Body:    core.BodyRelaxed,
			Handler: h.postPlan,
		},

		// The join. Observed half never stored, always stamped (§4.5).
		//
		// There is deliberately no /categories route: the vocabulary already
		// reaches every client through the published schema (schema.go), and a
		// second endpoint serving the same list would be the second source that
		// eventually disagrees with the first.
		{
			Method: "GET", Path: "/list", Tool: "show list",
			Summary: "List every device known on the network, joining stored identity to observed presence: " +
				"who is here now, what address they hold, and when they were last seen.",
			Handler: h.getList,
		},
	}
}

// Handler returns the module's routes.
func (h HTTP) Handler() http.Handler { return core.RouteTable(h.Routes()) }

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

// --- one item at a time ----------------------------------------------------

// groupBody is the body of PUT /groups/{name}. An omitted name means the one in
// the path, so creating a group does not have to say it twice.
type groupBody struct {
	Name string `json:"name"`
}

// putGroup creates a group, or renames one when the body names another.
//
// Creating one that already exists is a no-op rather than a conflict: the
// request says "there should be a group called this", and there is.
func (h HTTP) putGroup(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	var body groupBody
	if err := core.DecodeJSON(w, r, &body); err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	to := strings.TrimSpace(body.Name)
	if to == "" {
		to = name
	}

	h.mutate(w, r, func(cfg *Config) error {
		if to == name {
			if _, ok := cfg.FindGroup(name); !ok {
				cfg.UpsertGroup(Group{Name: name})
			}
			return nil
		}
		if _, ok := cfg.FindGroup(name); !ok {
			return notFound(fmt.Errorf("there is no group called %q; %s", name, knownGroups(*cfg)))
		}
		// Changing only the case of a name is a rename too, so the clash check
		// is on the exact name: "iot" → "IoT" must not collide with itself.
		if _, taken := cfg.FindGroup(to); taken {
			return badRequest(fmt.Errorf(
				"cannot rename %q to %q: there is already a group called %q", name, to, to))
		}
		cfg.RenameGroup(name, to)
		return nil
	})
}

func (h HTTP) deleteGroup(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	h.mutate(w, r, func(cfg *Config) error {
		if !cfg.RemoveGroup(name) {
			return notFound(fmt.Errorf("there is no group called %q; %s", name, knownGroups(*cfg)))
		}
		return nil
	})
}

// deviceGroupBody is the body of PUT /devices/{mac}/group. An empty group is a
// real value: it takes the device out of whatever group it was in.
type deviceGroupBody struct {
	Group string `json:"group"`
}

func (h HTTP) putDeviceGroup(w http.ResponseWriter, r *http.Request) {
	mac, err := core.NormalizeMAC(r.PathValue("mac"))
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	var body deviceGroupBody
	if err := core.DecodeJSON(w, r, &body); err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	h.mutate(w, r, func(cfg *Config) error {
		// Naming a group that does not exist is caught by Validate, with the
		// list of the ones that do.
		cfg.SetDeviceGroup(mac, strings.TrimSpace(body.Group))
		return nil
	})
}

// mutate loads the stored document, edits it and stores the result, all under
// the apply lock, so that the edit is made against the document it is saved
// over rather than one a concurrent writer has since replaced.
func (h HTTP) mutate(w http.ResponseWriter, r *http.Request, edit func(*Config) error) {
	var (
		stored   Config
		plan     planView
		applyErr error
		refused  error
		invalid  Result
	)
	if err := h.Lock.Do(r.Context(), func() error {
		current, err := h.Applier.Load()
		if err != nil {
			applyErr = err
			return nil
		}
		desired := current.Clone()
		if err := edit(&desired); err != nil {
			refused = err
			return nil
		}
		desired.Normalize()
		if res := Validate(desired); !res.OK() {
			invalid = res
			return nil
		}
		plan = buildPlan(current, desired)
		stored, applyErr = h.Applier.Save(desired)
		return nil
	}); err != nil {
		core.WriteError(w, http.StatusServiceUnavailable,
			"timed out waiting for the apply lock: "+err.Error())
		return
	}

	switch {
	case refused != nil:
		core.WriteError(w, statusFor(refused), refused.Error())
		return
	case !invalid.OK():
		core.WriteError(w, http.StatusUnprocessableEntity,
			"invalid devices configuration", problems(invalid.Errors)...)
		return
	case applyErr != nil:
		core.WriteJSON(w, http.StatusInternalServerError, applyResponse{
			Plan:   plan,
			Config: stored,
			Error:  &core.ErrorBody{Message: applyErr.Error()},
		})
		return
	}

	if !plan.Empty {
		h.Events.Publish(core.Event{Type: core.EventApplied, Module: ModuleName})
	}
	core.WriteJSON(w, http.StatusOK, applyResponse{Plan: plan, Config: stored})
}

// editError carries the status an edit's refusal deserves, the way
// internal/gateway/http.go does.
type editError struct {
	status int
	err    error
}

func (e editError) Error() string { return e.err.Error() }
func (e editError) Unwrap() error { return e.err }

func notFound(err error) error   { return editError{http.StatusNotFound, err} }
func badRequest(err error) error { return editError{http.StatusBadRequest, err} }

func statusFor(err error) int {
	var e editError
	if errors.As(err, &e) {
		return e.status
	}
	return http.StatusUnprocessableEntity
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
