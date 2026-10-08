package iptv

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

const ConfigPath = "/etc/open-linux-router/rendered/iptv/igmpproxy.conf"

type Applier struct {
	Store *core.Store
	Links Links
	Unit  core.Unit
	Root  string
}

func (a Applier) path() string { return filepath.Join(a.Root, ConfigPath) }
func (a Applier) Load() (Config, error) {
	doc, err := a.Store.Load()
	if err != nil {
		return Config{}, err
	}
	return FromDocument(doc)
}

type Plan struct {
	Changes []string `json:"changes"`
	Impact  string   `json:"impact"`
	Empty   bool     `json:"empty"`
}

func (a Applier) Plan(ctx context.Context, c Config) (Plan, error) {
	r, err := Resolve(c, a.Links)
	if err != nil {
		return Plan{}, err
	}
	if c.Enabled && a.Root == "" {
		forward, err := os.ReadFile("/proc/sys/net/ipv4/ip_forward")
		if err != nil {
			return Plan{}, fmt.Errorf("reading IPv4 forwarding: %w", err)
		}
		if string(bytes.TrimSpace(forward)) != "1" {
			return Plan{}, fmt.Errorf("IPTV requires IPv4 forwarding; enable the gateway or configure net.ipv4.ip_forward=1")
		}
	}
	plan := Plan{Empty: true, Impact: "none"}
	current, err := os.ReadFile(a.path())
	if err != nil && !os.IsNotExist(err) {
		return Plan{}, err
	}
	status, err := a.Unit.Status(ctx)
	if err != nil {
		return Plan{}, err
	}
	if c.Enabled {
		if !bytes.Equal(current, Render(r)) {
			plan.Changes = append(plan.Changes, "render igmpproxy configuration")
		}
		if !status.Enabled {
			plan.Changes = append(plan.Changes, "enable "+UnitName)
		}
		if !status.Active {
			plan.Changes = append(plan.Changes, "start "+UnitName)
		} else if !bytes.Equal(current, Render(r)) {
			plan.Changes = append(plan.Changes, "restart "+UnitName)
		}
	} else {
		if status.Active {
			plan.Changes = append(plan.Changes, "stop "+UnitName)
		}
		if status.Enabled {
			plan.Changes = append(plan.Changes, "disable "+UnitName)
		}
	}
	plan.Empty = len(plan.Changes) == 0
	if !plan.Empty {
		plan.Impact = "restart"
	}
	return plan, nil
}
func (a Applier) Apply(ctx context.Context, c Config, store bool) ([]core.Step, error) {
	r, err := Resolve(c, a.Links)
	if err != nil {
		return nil, err
	}
	if c.Enabled && a.Root == "" {
		if _, err := os.Stat("/usr/sbin/igmpproxy"); err != nil {
			return nil, fmt.Errorf("install igmpproxy before enabling IPTV: %w", err)
		}
	}
	plan, err := a.Plan(ctx, c)
	if err != nil {
		return nil, err
	}
	var steps []core.Step
	run := func(label string, fn func() error) error {
		e := fn()
		s := core.Step{Description: label, Done: e == nil}
		if e != nil {
			s.Error = e.Error()
		}
		steps = append(steps, s)
		return e
	}
	if store {
		if err := run("store IPTV configuration", func() error {
			doc, e := a.Store.Load()
			if e != nil {
				return e
			}
			raw, e := MarshalConfig(c)
			if e != nil {
				return e
			}
			doc.Set(ModuleName, raw)
			return a.Store.Save(doc)
		}); err != nil {
			return steps, err
		}
	}
	if plan.Empty {
		return steps, nil
	}
	status, err := a.Unit.Status(ctx)
	if err != nil {
		return steps, err
	}
	if !c.Enabled {
		if status.Active {
			if err := run("stop IPTV", func() error { return a.Unit.Stop(ctx) }); err != nil {
				return steps, err
			}
		}
		if status.Enabled {
			if err := run("disable IPTV", func() error { return a.Unit.Disable(ctx) }); err != nil {
				return steps, err
			}
		}
		return steps, nil
	}
	rendered := Render(r)
	existing, err := os.ReadFile(a.path())
	if err != nil && !os.IsNotExist(err) {
		return steps, err
	}
	changed := !bytes.Equal(existing, rendered)
	if changed {
		if err := run("render IPTV configuration", func() error { return core.WriteFileAtomic(a.path(), rendered, 0o644) }); err != nil {
			return steps, err
		}
	}
	if !status.Enabled {
		if err := run("enable IPTV", func() error { return a.Unit.Enable(ctx) }); err != nil {
			return steps, err
		}
	}
	if changed && status.Active {
		err = run("restart IPTV", func() error { return a.Unit.Restart(ctx) })
	} else if !status.Active {
		err = run("start IPTV", func() error { return a.Unit.Start(ctx) })
	}
	if err != nil {
		return steps, fmt.Errorf("IPTV backend: %w", err)
	}
	return steps, nil
}
