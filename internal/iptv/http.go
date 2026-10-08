package iptv

import (
	"context"
	"net/http"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

type HTTP struct {
	Applier Applier
	Lock    *core.Lock
	Events  *core.Events
	Follow  func(context.Context) error
}

func (h HTTP) Routes() []core.Route {
	return []core.Route{
		{Method: "GET", Path: "/config", Tool: "show config", Summary: "Show routed IPTV multicast settings.", Handler: h.getConfig},
		{Method: "PUT", Path: "/config", Body: core.BodyFull, Mutating: true, Summary: "Replace IPTV multicast settings.", Handler: h.putConfig},
		{Method: "POST", Path: "/plan", Tool: "show plan", Body: core.BodyRelaxed, Summary: "Preview IPTV backend changes without applying them.", Handler: h.plan},
		{Method: "POST", Path: "/apply", Mutating: true, Summary: "Reapply stored IPTV settings.", Handler: h.reapply},
		{Method: "GET", Path: "/status", Tool: "status", Summary: "Show IPTV service state and drift.", Handler: h.status},
	}
}
func (h HTTP) getConfig(w http.ResponseWriter, _ *http.Request) {
	c, e := h.Applier.Load()
	if e != nil {
		core.WriteError(w, 500, e.Error())
		return
	}
	core.WriteJSON(w, 200, c)
}
func (h HTTP) putConfig(w http.ResponseWriter, r *http.Request) {
	raw, e := core.ReadBody(w, r)
	if e != nil {
		core.WriteError(w, 400, e.Error())
		return
	}
	c, e := UnmarshalConfig(raw)
	if e != nil {
		core.WriteError(w, 400, e.Error())
		return
	}
	h.change(w, r, c, true)
}
func (h HTTP) reapply(w http.ResponseWriter, r *http.Request) {
	c, e := h.Applier.Load()
	if e != nil {
		core.WriteError(w, 500, e.Error())
		return
	}
	h.change(w, r, c, false)
}
func (h HTTP) change(w http.ResponseWriter, r *http.Request, c Config, store bool) {
	var plan Plan
	var steps []core.Step
	var failure error
	if err := h.Lock.Do(r.Context(), func() error {
		plan, failure = h.Applier.Plan(r.Context(), c)
		if failure == nil {
			steps, failure = h.Applier.Apply(r.Context(), c, store)
		}
		if h.Follow != nil {
			if err := h.Follow(r.Context()); err != nil && failure == nil {
				failure = err
			}
		}
		return nil
	}); err != nil {
		core.WriteError(w, 503, err.Error())
		return
	}
	if failure != nil {
		core.WriteJSON(w, 500, map[string]any{"plan": plan, "steps": steps, "error": core.ErrorBody{Message: failure.Error()}})
		return
	}
	if h.Events != nil {
		h.Events.Publish(core.Event{Type: core.EventApplied, Module: ModuleName})
	}
	core.WriteJSON(w, 200, map[string]any{"plan": plan, "steps": steps})
}
func (h HTTP) plan(w http.ResponseWriter, r *http.Request) {
	raw, e := core.ReadBody(w, r)
	if e != nil {
		core.WriteError(w, 400, e.Error())
		return
	}
	var c Config
	if len(raw) == 0 {
		c, e = h.Applier.Load()
	} else {
		c, e = UnmarshalConfig(raw)
	}
	if e != nil {
		core.WriteError(w, 400, e.Error())
		return
	}
	p, e := h.Applier.Plan(r.Context(), c)
	if e != nil {
		core.WriteError(w, 400, e.Error())
		return
	}
	core.WriteJSON(w, 200, p)
}
func (h HTTP) status(w http.ResponseWriter, r *http.Request) {
	c, e := h.Applier.Load()
	if e != nil {
		core.WriteError(w, 500, e.Error())
		return
	}
	s, e := h.Applier.Unit.Status(r.Context())
	if e != nil {
		core.WriteError(w, 500, e.Error())
		return
	}
	p, e := h.Applier.Plan(r.Context(), c)
	if e != nil {
		core.WriteJSON(w, 200, map[string]any{"enabled": c.Enabled, "service": s, "problem": e.Error()})
		return
	}
	core.WriteJSON(w, 200, map[string]any{"enabled": c.Enabled, "service": s, "plan": p})
}
