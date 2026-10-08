package socksout

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

type Applier struct {
	Store *core.Store
	Unit  core.Unit
	Root  string
}

func (a Applier) path() string { return filepath.Join(a.Root, DefaultConfig) }
func (a Applier) Load() (Config, error) {
	d, err := a.Store.Load()
	if err != nil {
		return Config{}, err
	}
	return FromDocument(d)
}

type Plan struct {
	Changes []string `json:"changes"`
	Impact  string   `json:"impact"`
	Empty   bool     `json:"empty"`
}

func (a Applier) Plan(ctx context.Context, c Config) (Plan, error) {
	if err := c.Validate(); err != nil {
		return Plan{}, err
	}
	status, err := a.Unit.Status(ctx)
	if err != nil {
		return Plan{}, err
	}
	p := Plan{Empty: true, Impact: "none"}
	if c.Enabled {
		want, _ := json.Marshal(c)
		have, err := os.ReadFile(a.path())
		if err != nil && !os.IsNotExist(err) {
			return p, err
		}
		if !bytes.Equal(want, have) {
			p.Changes = append(p.Changes, "update SOCKS5 connection")
		}
		if !status.Enabled {
			p.Changes = append(p.Changes, "enable "+UnitName)
		}
		if !status.Active {
			p.Changes = append(p.Changes, "start "+UnitName)
		} else if !bytes.Equal(want, have) {
			p.Changes = append(p.Changes, "restart "+UnitName)
		}
	} else {
		if status.Active {
			p.Changes = append(p.Changes, "stop "+UnitName)
		}
		if status.Enabled {
			p.Changes = append(p.Changes, "disable "+UnitName)
		}
	}
	p.Empty = len(p.Changes) == 0
	if !p.Empty {
		p.Impact = "restart"
	}
	return p, nil
}
func (a Applier) Apply(ctx context.Context, c Config, store bool) ([]core.Step, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if _, err := a.Plan(ctx, c); err != nil {
		return nil, err
	}
	var steps []core.Step
	run := func(label string, fn func() error) error {
		err := fn()
		step := core.Step{Description: label, Done: err == nil}
		if err != nil {
			step.Error = err.Error()
		}
		steps = append(steps, step)
		return err
	}
	if store {
		if err := run("save SOCKS5 outbound settings", func() error {
			d, err := a.Store.Load()
			if err != nil {
				return err
			}
			raw, err := json.Marshal(c)
			if err != nil {
				return err
			}
			d.Set(ModuleName, raw)
			return a.Store.Save(d)
		}); err != nil {
			return steps, err
		}
	}
	status, err := a.Unit.Status(ctx)
	if err != nil {
		return steps, err
	}
	if !c.Enabled {
		if status.Active {
			if err := run("stop SOCKS5 outbound", func() error { return a.Unit.Stop(ctx) }); err != nil {
				return steps, err
			}
		}
		if status.Enabled {
			if err := run("disable SOCKS5 outbound", func() error { return a.Unit.Disable(ctx) }); err != nil {
				return steps, err
			}
		}
		return steps, nil
	}
	data, _ := json.Marshal(c)
	old, err := os.ReadFile(a.path())
	if err != nil && !os.IsNotExist(err) {
		return steps, err
	}
	changed := !bytes.Equal(old, data)
	if changed {
		if err := run("render SOCKS5 outbound settings", func() error { return core.WriteFileAtomic(a.path(), data, 0o600) }); err != nil {
			return steps, err
		}
	}
	if !status.Enabled {
		if err := run("enable SOCKS5 outbound", func() error { return a.Unit.Enable(ctx) }); err != nil {
			return steps, err
		}
	}
	if changed && status.Active {
		err = run("restart SOCKS5 outbound", func() error { return a.Unit.Restart(ctx) })
	} else if !status.Active {
		err = run("start SOCKS5 outbound", func() error { return a.Unit.Start(ctx) })
	}
	if err != nil {
		return steps, fmt.Errorf("starting SOCKS5 outbound: %w", err)
	}
	return steps, nil
}
