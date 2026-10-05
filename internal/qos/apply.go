package qos

import (
	"encoding/json"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

type Applier struct{ Store *core.Store }
type Status struct {
	Enabled bool   `json:"enabled"`
	Active  bool   `json:"active"`
	Reason  string `json:"reason,omitempty"`
}

func (a Applier) Load() (Config, error) {
	d, err := a.Store.Load()
	if err != nil {
		return Config{}, err
	}
	return FromDocument(d)
}
func (a Applier) Save(c Config) error {
	c.Normalize()
	if err := c.Validate(); err != nil {
		return err
	}
	d, err := a.Store.Load()
	if err != nil {
		return err
	}
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	d.Set(ModuleName, b)
	return a.Store.Save(d)
}
func (a Applier) Status() Status {
	c, err := a.Load()
	if err != nil {
		return Status{Reason: err.Error()}
	}
	return Status{Enabled: c.Enabled, Active: false, Reason: "Device QoS enforcement is not available yet"}
}
