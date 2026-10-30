package gateway

import (
	"errors"
	"fmt"
	"slices"
)

var errLatencySiteNotFound = errors.New("unknown monitored site")

// SetPinned updates only homepage selection. Callers hold the global apply
// lock across the load and save, just as for Replace and SetTarget.
// The returned bool reports whether the stored selection changed.
func (m *CustomLatencyMonitor) SetPinned(name string, pinned bool) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.store == nil {
		return false, errors.New("latency pin configuration unavailable")
	}
	doc, err := m.store.Load()
	if err != nil {
		return false, err
	}
	cfg, err := FromDocument(doc)
	if err != nil {
		return false, err
	}
	known := false
	if _, ok := commonLatencySite(name); ok {
		known = true
	}
	for _, site := range cfg.LatencySites {
		if site.Name == name {
			known = true
			break
		}
	}
	if !known {
		return false, fmt.Errorf("%w: %q", errLatencySiteNotFound, name)
	}
	if slices.Contains(cfg.LatencyPins, name) == pinned {
		m.pins = append([]string{}, cfg.LatencyPins...)
		return false, nil
	}
	if pinned {
		cfg.LatencyPins = append(cfg.LatencyPins, name)
	} else {
		cfg.LatencyPins = slices.DeleteFunc(cfg.LatencyPins, func(pin string) bool { return pin == name })
	}
	cfg.Normalize()
	data, err := MarshalConfig(cfg)
	if err != nil {
		return false, err
	}
	doc.Set(ModuleName, data)
	if err := m.store.Save(doc); err != nil {
		return false, err
	}
	m.pins = append([]string{}, cfg.LatencyPins...)
	return true, nil
}
