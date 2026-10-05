package qos

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

type HTTP struct {
	Applier Applier
	Lock    *core.Lock
	Events  *core.Events
}

func (h HTTP) Routes() []core.Route {
	return []core.Route{
		{Method: "GET", Path: "/config", Tool: "show config", Summary: "Show the saved network priority and speed limits.", Handler: h.getConfig},
		{Method: "GET", Path: "/status", Tool: "status", Summary: "Check whether device QoS is effective on the router.", Handler: h.getStatus},
		{Method: "PUT", Path: "/config", Body: core.BodyFull, Mutating: true, Summary: "Replace and apply all QoS settings.", Handler: h.putConfig},
		{Method: "PUT", Path: "/devices/{mac}", Mutating: true, Summary: "Set one device's network priority and speed limits.", Handler: h.putDevice},
		{Method: "DELETE", Path: "/devices/{mac}", Mutating: true, Summary: "Reset one device to normal priority and no speed limit.", Handler: h.deleteDevice},
	}
}
func (h HTTP) getConfig(w http.ResponseWriter, r *http.Request) {
	c, e := h.Applier.Load()
	if e != nil {
		core.WriteError(w, 500, e.Error())
		return
	}
	core.WriteJSON(w, 200, c)
}
func (h HTTP) getStatus(w http.ResponseWriter, r *http.Request) {
	core.WriteJSON(w, 200, h.Applier.Status())
}
func (h HTTP) putConfig(w http.ResponseWriter, r *http.Request) {
	b, e := core.ReadBody(w, r)
	if e != nil {
		core.WriteError(w, 400, e.Error())
		return
	}
	c, e := Parse(b)
	if e != nil {
		core.WriteError(w, 422, e.Error())
		return
	}
	h.apply(w, r, func(*Config) error { return nil }, &c)
}
func (h HTTP) putDevice(w http.ResponseWriter, r *http.Request) {
	b, e := core.ReadBody(w, r)
	if e != nil {
		core.WriteError(w, 400, e.Error())
		return
	}
	var d Device
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if e = dec.Decode(&d); e != nil {
		core.WriteError(w, 400, e.Error())
		return
	}
	if e = dec.Decode(new(any)); e != io.EOF {
		core.WriteError(w, 400, "extra JSON after device policy")
		return
	}
	if d.MAC != "" {
		bodyMAC, bodyErr := core.NormalizeMAC(d.MAC)
		pathMAC, pathErr := core.NormalizeMAC(r.PathValue("mac"))
		if bodyErr != nil || pathErr != nil || bodyMAC != pathMAC {
			core.WriteError(w, 422, "device MAC in body must match the path")
			return
		}
	}
	d.MAC = r.PathValue("mac")
	if e = (Config{Devices: []Device{d}}).Validate(); e != nil {
		core.WriteError(w, 422, e.Error())
		return
	}
	h.apply(w, r, func(c *Config) error { c.SetDevice(d); return nil }, nil)
}
func (h HTTP) deleteDevice(w http.ResponseWriter, r *http.Request) {
	if _, err := core.NormalizeMAC(r.PathValue("mac")); err != nil {
		core.WriteError(w, 422, err.Error())
		return
	}
	h.apply(w, r, func(c *Config) error { c.RemoveDevice(r.PathValue("mac")); return nil }, nil)
}
func (h HTTP) apply(w http.ResponseWriter, r *http.Request, edit func(*Config) error, replacement *Config) {
	var c Config
	var problem error
	invalid := false
	e := h.Lock.Do(r.Context(), func() error {
		if replacement != nil {
			c = *replacement
		} else {
			c, problem = h.Applier.Load()
			if problem != nil {
				return nil
			}
			problem = edit(&c)
		}
		if problem != nil {
			return nil
		}
		if problem = c.Validate(); problem != nil {
			invalid = true
			return nil
		}
		problem = h.Applier.Save(c)
		if problem != nil {
			return nil
		}
		return nil
	})
	if e != nil {
		core.WriteError(w, 503, e.Error())
		return
	}
	if problem != nil {
		code := 500
		if invalid {
			code = 422
		}
		core.WriteError(w, code, problem.Error())
		return
	}
	if h.Events != nil {
		h.Events.Publish(core.Event{Type: core.EventApplied, Module: ModuleName})
	}
	core.WriteJSON(w, 200, map[string]any{"config": c, "status": h.Applier.Status()})
}
