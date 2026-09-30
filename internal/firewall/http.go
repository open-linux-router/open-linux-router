package firewall

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strconv"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// HTTP is this module's REST surface: the same applier, lock and event bus
// every module has.
type HTTP struct {
	Applier Applier
	Lock    *core.Lock
	Events  *core.Events
}

// Routes is the module's surface. Core mounts it under /api/firewall.
func (h HTTP) Routes() []core.Route {
	// The gate every mutating route carries (§6.2): ask first with dry_run,
	// go ahead with a disruptive plan only with confirm.
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
		{
			Method: "GET", Path: "/config", Tool: "show config",
			Summary: "Show whether the firewall is on.",
			Handler: h.getConfig,
		},
		{
			Method: "PUT", Path: "/config",
			Summary:  "Replace the firewall configuration.",
			Body:     core.BodyFull,
			Query:    gate,
			Mutating: true,
			Handler:  h.putConfig,
		},
		{
			Method: "PATCH", Path: "/config",
			Summary:  "Turn the firewall on or off.",
			Body:     core.BodyRelaxed,
			Query:    gate,
			Mutating: true,
			Handler:  h.patchConfig,
		},
		{
			Method: "POST", Path: "/plan", Tool: "show plan",
			Summary: "Show what applying a firewall configuration would change, without changing it.",
			Body:    core.BodyRelaxed,
			Handler: h.postPlan,
		},
		{
			Method: "POST", Path: "/apply",
			Summary:  "Program the stored firewall configuration again, repairing any drift.",
			Mutating: true,
			Handler:  h.postApply,
		},
		{
			Method: "GET", Path: "/status", Tool: "status",
			Summary: "Show whether the firewall is in force, what it lets in from outside, " +
				"and how much it has blocked.",
			Handler: h.getStatus,
		},
	}
}

// Handler returns the module's routes.
func (h HTTP) Handler() http.Handler { return core.RouteTable(h.Routes()) }

type applyResponse struct {
	Plan   Plan            `json:"plan"`
	Config Config          `json:"config"`
	Steps  []core.Step     `json:"steps,omitempty"`
	Error  *core.ErrorBody `json:"error,omitempty"`
}

func (h HTTP) getConfig(w http.ResponseWriter, _ *http.Request) {
	c, err := h.Applier.Load()
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	core.WriteJSON(w, http.StatusOK, c)
}

func (h HTTP) putConfig(w http.ResponseWriter, r *http.Request) {
	data, err := core.ReadBody(w, r)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	c, err := UnmarshalConfig(data)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.mutate(w, r, false, func(*Config) (Config, error) { return c, nil })
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
	h.mutate(w, r, false, func(current *Config) (Config, error) {
		return merge(*current, patch)
	})
}

// postApply re-programs what is stored. Confirmed by construction: the
// operator decided when they stored it, and a drifted box is the one that most
// needs repairing without an extra question.
func (h HTTP) postApply(w http.ResponseWriter, r *http.Request) {
	h.mutate(w, r, true, func(current *Config) (Config, error) { return *current, nil })
}

func merge(current Config, patch []byte) (Config, error) {
	raw, err := MarshalConfig(current)
	if err != nil {
		return Config{}, err
	}
	merged, err := core.MergePatch(raw, patch)
	if err != nil {
		return Config{}, err
	}
	return UnmarshalConfig(merged)
}

// mutate is every write: edit, plan, then either stop with the plan or apply.
func (h HTTP) mutate(w http.ResponseWriter, r *http.Request, forceConfirm bool,
	edit func(*Config) (Config, error)) {
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
	via := h.via(r)

	var (
		status          = http.StatusOK
		failure         string
		resp            applyResponse
		held, planned   bool
		editErr, badErr error
	)
	if err := h.Lock.Do(r.Context(), func() error {
		current, err := h.Applier.Load()
		if err != nil {
			status, failure = http.StatusInternalServerError, err.Error()
			return nil
		}
		next, err := edit(&current)
		if err != nil {
			editErr = err
			return nil
		}
		plan, desired, err := h.Applier.Plan(r.Context(), next, via)
		if err != nil {
			badErr = err
			return nil
		}
		planned = true
		resp.Plan = plan

		if dryRun || (plan.Impact == ImpactDisruptive && !confirm) {
			held = true
			resp.Config = current
			return nil
		}

		resp.Config = next
		resp.Steps, err = h.Applier.ApplyPlanned(r.Context(), next, plan, desired)
		if err != nil {
			status = http.StatusInternalServerError
			resp.Error = &core.ErrorBody{Message: err.Error()}
		}
		return nil
	}); err != nil {
		core.WriteError(w, http.StatusServiceUnavailable,
			"timed out waiting for the apply lock: "+err.Error())
		return
	}

	switch {
	case failure != "":
		core.WriteError(w, status, failure)
		return
	case editErr != nil:
		core.WriteError(w, http.StatusBadRequest, editErr.Error())
		return
	case badErr != nil:
		writePlanError(w, badErr)
		return
	case !planned:
		return
	case dryRun:
		core.WriteJSON(w, http.StatusOK, resp.Plan)
		return
	case held:
		resp.Error = &core.ErrorBody{Message: "this would cut off your own connection to the router; " +
			"repeat the request with confirm=true to go ahead"}
		core.WriteJSON(w, http.StatusConflict, resp)
		return
	}

	if resp.Error == nil && !resp.Plan.Empty {
		h.Events.Publish(core.Event{Type: core.EventApplied, Module: ModuleName})
	}
	core.WriteJSON(w, status, resp)
}

func (h HTTP) postPlan(w http.ResponseWriter, r *http.Request) {
	data, err := core.ReadBody(w, r)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	c, err := h.Applier.Load()
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(bytes.TrimSpace(data)) > 0 {
		if c, err = merge(c, data); err != nil {
			core.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	plan, _, err := h.Applier.Plan(r.Context(), c, h.via(r))
	if err != nil {
		writePlanError(w, err)
		return
	}
	core.WriteJSON(w, http.StatusOK, plan)
}

func (h HTTP) getStatus(w http.ResponseWriter, r *http.Request) {
	st, err := h.Applier.GetStatus(r.Context())
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	core.WriteJSON(w, http.StatusOK, st)
}

func writePlanError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrNoInside) {
		core.WriteError(w, http.StatusUnprocessableEntity, "invalid firewall configuration",
			core.Problem{Path: "enabled", Message: err.Error()})
		return
	}
	core.WriteError(w, http.StatusInternalServerError, err.Error())
}

// via is the interface holding the address the request arrived at — the one
// the caller will need to reach again after the apply. Empty over the unix
// socket, which no ruleset can cut off.
func (h HTTP) via(r *http.Request) string {
	local, _ := r.Context().Value(http.LocalAddrContextKey).(net.Addr)
	tcp, ok := local.(*net.TCPAddr)
	if !ok || h.Applier.Boundary == nil {
		return ""
	}
	addr, ok := netip.AddrFromSlice(tcp.IP)
	if !ok {
		return ""
	}
	addr = addr.Unmap()
	if addr.IsLoopback() {
		return "lo"
	}
	name, _ := h.Applier.Boundary.InterfaceOf(addr)
	return name
}

// boolParam reads a flag-style query parameter; bare presence is true.
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
