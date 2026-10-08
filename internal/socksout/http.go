package socksout

import (
	"context"
	"net/http"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

type HTTP struct {
	Applier Applier
	Lock    *core.Lock
	Events  *core.Events
	// Follow refreshes a gateway exit which references this TUN.
	Follow func(context.Context) error
}

func (h HTTP) Routes() []core.Route {
	return []core.Route{
		{Method: "GET", Path: "/config", Tool: "show config", Summary: "Show outbound SOCKS5 settings.", Handler: h.get},
		{Method: "PUT", Path: "/config", Body: core.BodyFull, Mutating: true, Summary: "Configure the outbound SOCKS5 TUN.", Handler: h.put},
		{Method: "POST", Path: "/plan", Body: core.BodyRelaxed, Tool: "show plan", Summary: "Preview outbound SOCKS5 changes.", Handler: h.plan},
		{Method: "POST", Path: "/apply", Mutating: true, Summary: "Reapply stored outbound SOCKS5 settings.", Handler: h.reapply},
		{Method: "GET", Path: "/status", Tool: "status", Summary: "Show outbound SOCKS5 backend state.", Handler: h.status},
	}
}
func (h HTTP) get(w http.ResponseWriter, _ *http.Request) {
	c, e := h.Applier.Load()
	if e != nil {
		core.WriteError(w, 500, e.Error())
		return
	}
	core.WriteJSON(w, 200, c)
}
func (h HTTP) put(w http.ResponseWriter, r *http.Request) {
	raw, e := core.ReadBody(w, r)
	if e != nil {
		core.WriteError(w, 400, e.Error())
		return
	}
	c, e := Parse(raw)
	if e != nil {
		core.WriteError(w, 422, e.Error())
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
	var p Plan
	var steps []core.Step
	var failure error
	if err := h.Lock.Do(r.Context(), func() error {
		p, failure = h.Applier.Plan(r.Context(), c)
		if failure == nil {
			steps, failure = h.Applier.Apply(r.Context(), c, store)
		}
		if h.Follow != nil && failure == nil {
			failure = h.Follow(r.Context())
		}
		return nil
	}); err != nil {
		core.WriteError(w, 503, err.Error())
		return
	}
	if failure != nil {
		core.WriteJSON(w, 500, map[string]any{"plan": p, "steps": steps, "error": core.ErrorBody{Message: failure.Error()}})
		return
	}
	if h.Events != nil {
		h.Events.Publish(core.Event{Type: core.EventApplied, Module: ModuleName})
	}
	core.WriteJSON(w, 200, map[string]any{"plan": p, "steps": steps})
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
		c, e = Parse(raw)
	}
	if e != nil {
		core.WriteError(w, 400, e.Error())
		return
	}
	p, e := h.Applier.Plan(r.Context(), c)
	if e != nil {
		core.WriteError(w, 422, e.Error())
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
