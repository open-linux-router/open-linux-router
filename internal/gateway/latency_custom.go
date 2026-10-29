package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

const maxCustomLatencySites = 20

// Categories are stable keys, not labels: the UI translates them, and a saved
// document must keep its grouping when the interface language changes. Empty
// means a site the operator added by hand, which the UI shows as Custom.
var latencyCategories = map[string]bool{
	"communication": true,
	"social":        true,
	"entertainment": true,
	"news":          true,
	"shopping":      true,
	"tools":         true,
}

type CustomLatencySite struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Exit     string `json:"exit,omitempty"`
	Icon     string `json:"icon,omitempty"`
	Category string `json:"category,omitempty"`
}

// Custom targets are independent of the Internet candidate selection.
// Their intent survives restarts, while measurements remain ephemeral.
type CustomLatencyMonitor struct {
	mu           sync.RWMutex
	path         string
	store        *core.Store
	sites        []CustomLatencySite
	results      []LatencySite
	icons        map[string]latencyIcon
	probe        func(context.Context, string) (float64, error)
	probeThrough func(context.Context, string, uint32) (float64, error)
	iconClient   func(*CustomLatencySite) (*http.Client, error)
	// inflight marks on-demand probes, keyed by URL and exit. Background rounds
	// never enter it, so only a clicked site reports progress.
	inflight map[string]bool
	// base is the daemon lifetime. On-demand probes start in a request handler,
	// so they cannot use its context and follow this one instead.
	base context.Context
	// Resolve returns the current exit mark; a missing or disabled exit is an error.
	Resolve      func(string) (uint32, error)
	ValidateExit func(string) error
}

// OpenCustomLatencyConfig moves legacy targets into the operator document.
// It runs before serving requests; subsequent writes hold the global apply lock.
func OpenCustomLatencyConfig(store *core.Store) (*CustomLatencyMonitor, error) {
	doc, err := store.Load()
	if err != nil {
		return nil, err
	}
	cfg, err := FromDocument(doc)
	if err != nil {
		return nil, err
	}
	legacy := store.Path() + ".latency-sites"
	if cfg.LatencySites == nil {
		data, err := os.ReadFile(legacy)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		if err == nil {
			if err := json.Unmarshal(data, &cfg.LatencySites); err != nil {
				return nil, err
			}
			if err := validateCustomSites(cfg.LatencySites); err != nil {
				return nil, err
			}
			// A non-nil empty slice records migration, including cleared targets.
			if cfg.LatencySites == nil {
				cfg.LatencySites = []CustomLatencySite{}
			}
			raw, err := MarshalConfig(cfg)
			if err != nil {
				return nil, err
			}
			doc.Set(ModuleName, raw)
			if err := store.Save(doc); err != nil {
				return nil, err
			}
		}
	}
	// Only remove the legacy source after the new configuration is committed.
	// An explicit empty list must never resurrect old targets on restart.
	if cfg.LatencySites != nil {
		if err := os.Rename(legacy, legacy+".migrated"); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	m := &CustomLatencyMonitor{store: store, probe: probePage, probeThrough: probePageMarked, results: []LatencySite{}, inflight: map[string]bool{}}
	m.sites = cfg.LatencySites
	m.resetResults()
	return m, nil
}

func NewCustomLatencyMonitor(path string) (*CustomLatencyMonitor, error) {
	m := &CustomLatencyMonitor{path: path, probe: probePage, probeThrough: probePageMarked, results: []LatencySite{}, inflight: map[string]bool{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &m.sites); err != nil {
		return nil, fmt.Errorf("custom latency sites: %w", err)
	}
	if err := validateCustomSites(m.sites); err != nil {
		return nil, err
	}
	m.resetResults()
	return m, nil
}

func validateCustomSites(sites []CustomLatencySite) error {
	if len(sites) > maxCustomLatencySites {
		return fmt.Errorf("at most %d sites are allowed", maxCustomLatencySites)
	}
	names := make(map[string]bool)
	for _, site := range sites {
		name := strings.TrimSpace(site.Name)
		if name == "" || len(name) > 40 {
			return errors.New("site name must be 1-40 characters")
		}
		if err := validateLatencyIcon(site.Icon); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if names[strings.ToLower(name)] {
			return fmt.Errorf("duplicate site name: %s", name)
		}
		names[strings.ToLower(name)] = true
		if site.Category != "" && !latencyCategories[site.Category] {
			return fmt.Errorf("%s: unknown category %q", name, site.Category)
		}
		u, err := url.Parse(site.URL)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.Opaque != "" || u.Port() != "" {
			return fmt.Errorf("%s: use an HTTPS URL without credentials, port, or fragment", name)
		}
	}
	return nil
}

func (m *CustomLatencyMonitor) resetResults() {
	previous := make(map[string]LatencySite, len(m.results))
	for _, result := range m.results {
		previous[result.URL+"\x00"+result.Exit] = result
	}
	m.results = make([]LatencySite, len(m.sites))
	for i, site := range m.sites {
		result := LatencySite{Name: site.Name, URL: site.URL, Exit: site.Exit, Icon: site.Icon, Category: site.Category}
		if old, ok := previous[site.URL+"\x00"+site.Exit]; ok {
			result.Milliseconds, result.CheckedAt, result.Error = old.Milliseconds, old.CheckedAt, old.Error
		}
		// A reorder or rename during an on-demand probe keeps its spinner on the
		// row that still names the same target.
		result.Measuring = m.inflight[site.URL+"\x00"+site.Exit]
		m.results[i] = result
	}
}

// setContext makes on-demand probes follow the daemon.
func (m *CustomLatencyMonitor) setContext(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.base = ctx
}

// Measure probes one stored site now and marks only that row as measuring.
//
// It returns without waiting: the result replaces the stored one when it
// arrives. A second click while the same target is probing is a no-op, so a
// slow site cannot accumulate parallel probes.
func (m *CustomLatencyMonitor) Measure(name string) error {
	m.refreshConfig()
	var site CustomLatencySite
	found := false
	for _, candidate := range m.Config() {
		if candidate.Name == name {
			site, found = candidate, true
			break
		}
	}
	if !found {
		return fmt.Errorf("no monitored site named %q", name)
	}
	key := site.URL + "\x00" + site.Exit
	m.mu.Lock()
	if m.inflight[key] {
		m.mu.Unlock()
		return nil
	}
	if m.inflight == nil {
		m.inflight = map[string]bool{}
	}
	m.inflight[key] = true
	for i := range m.results {
		if m.results[i].URL == site.URL && m.results[i].Exit == site.Exit {
			m.results[i].Measuring = true
		}
	}
	base := m.base
	m.mu.Unlock()
	if base == nil {
		base = context.Background()
	}
	go m.probeOne(base, site)
	return nil
}

// probeOne runs one on-demand probe and publishes its outcome.
func (m *CustomLatencyMonitor) probeOne(ctx context.Context, site CustomLatencySite) {
	bounded, cancel := context.WithTimeout(ctx, pageProbeTimeout)
	defer cancel()
	var value float64
	var err error
	if site.Exit == "" {
		value, err = m.probe(bounded, site.URL)
	} else {
		var mark uint32
		if m.Resolve == nil {
			err = errors.New("gateway exit unavailable")
		} else {
			mark, err = m.Resolve(site.Exit)
		}
		if err == nil {
			value, err = m.probeThrough(bounded, site.URL, mark)
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := site.URL + "\x00" + site.Exit
	delete(m.inflight, key)
	if ctx.Err() != nil {
		// Shutdown, not a probe failure: clear progress without recording a
		// measurement that never ran to completion.
		for i := range m.results {
			if m.results[i].URL == site.URL && m.results[i].Exit == site.Exit {
				m.results[i].Measuring = false
			}
		}
		return
	}
	result := LatencySite{Name: site.Name, URL: site.URL, Exit: site.Exit, Icon: site.Icon, Category: site.Category, CheckedAt: time.Now().UTC()}
	if site.Exit != "" && err != nil && strings.Contains(err.Error(), "gateway exit") {
		result.Error = "Gateway exit unavailable"
	}
	if err == nil {
		result.Milliseconds = &value
	} else if result.Error == "" {
		result.Error = latencyError(err)
	}
	m.storeResult(result)
}

// storeResult writes one outcome, keeping whichever attempt is newer. Callers
// hold m.mu. Two probes of the same target can overlap — a background round and
// a click — and the older one finishing last must not overwrite the newer one.
func (m *CustomLatencyMonitor) storeResult(result LatencySite) {
	key := result.URL + "\x00" + result.Exit
	for i, site := range m.sites {
		if site.URL != result.URL || site.Exit != result.Exit {
			continue
		}
		if current := m.results[i]; !current.CheckedAt.IsZero() && current.CheckedAt.After(result.CheckedAt) {
			return
		}
		result.Name = site.Name
		result.Icon = site.Icon
		result.Category = site.Category
		result.Measuring = m.inflight[key]
		m.results[i] = result
	}
}

func (m *CustomLatencyMonitor) Snapshot() []LatencySite {
	m.refreshConfig()
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]LatencySite{}, m.results...)
}

func (m *CustomLatencyMonitor) refreshConfig() {
	if m.store == nil {
		return
	}
	sites := m.Config()
	m.mu.Lock()
	defer m.mu.Unlock()
	old, _ := json.Marshal(m.sites)
	next, _ := json.Marshal(sites)
	if string(old) != string(next) {
		m.sites = sites
		m.resetResults()
		m.icons = nil
	}
}

func (m *CustomLatencyMonitor) Config() []CustomLatencySite {
	if m.store != nil {
		doc, err := m.store.Load()
		if err == nil {
			cfg, err := FromDocument(doc)
			if err == nil {
				return append([]CustomLatencySite{}, cfg.LatencySites...)
			}
		}
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]CustomLatencySite{}, m.sites...)
}

func (m *CustomLatencyMonitor) Replace(sites []CustomLatencySite) error {
	if err := validateCustomSites(sites); err != nil {
		return err
	}
	for _, site := range sites {
		if site.Exit != "" {
			if m.ValidateExit == nil {
				return errors.New("gateway exit validation unavailable")
			}
			if err := m.ValidateExit(site.Exit); err != nil {
				return err
			}
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.store != nil {
		doc, err := m.store.Load()
		if err != nil {
			return err
		}
		cfg, err := FromDocument(doc)
		if err != nil {
			return err
		}
		cfg.LatencySites = append([]CustomLatencySite{}, sites...)
		data, err := MarshalConfig(cfg)
		if err != nil {
			return err
		}
		doc.Set(ModuleName, data)
		if err := m.store.Save(doc); err != nil {
			return err
		}
		m.sites = append([]CustomLatencySite{}, sites...)
		m.resetResults()
		m.icons = nil
		return nil
	}
	data, err := json.MarshalIndent(sites, "", "  ")
	if err != nil {
		return err
	}
	if err := core.WriteFileAtomic(m.path, append(data, '\n'), 0o644); err != nil {
		return err
	}
	m.sites = append([]CustomLatencySite{}, sites...)
	m.resetResults()
	m.icons = nil
	return nil
}

func (m *CustomLatencyMonitor) sample(ctx context.Context) {
	m.refreshConfig()
	sites := m.Config()
	results := make([]LatencySite, len(sites))
	var wg sync.WaitGroup
	for i, site := range sites {
		wg.Add(1)
		go func(i int, site CustomLatencySite) {
			defer wg.Done()
			bounded, cancel := context.WithTimeout(ctx, pageProbeTimeout)
			defer cancel()
			var value float64
			var err error
			if site.Exit == "" {
				value, err = m.probe(bounded, site.URL)
			} else {
				var mark uint32
				if m.Resolve == nil {
					err = errors.New("gateway exit unavailable")
				} else {
					mark, err = m.Resolve(site.Exit)
				}
				if err == nil {
					value, err = m.probeThrough(bounded, site.URL, mark)
				}
			}
			results[i] = LatencySite{Name: site.Name, URL: site.URL, Exit: site.Exit, Icon: site.Icon, Category: site.Category, CheckedAt: time.Now().UTC()}
			if site.Exit != "" && err != nil && strings.Contains(err.Error(), "gateway exit") {
				results[i].Error = "Gateway exit unavailable"
			}
			if err == nil {
				results[i].Milliseconds = &value
			} else if results[i].Error == "" {
				results[i].Error = latencyError(err)
			}
		}(i, site)
	}
	wg.Wait()
	if ctx.Err() != nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	// Reconcile by target rather than index: renames and reorders must not
	// discard valid samples or attach a result to the wrong site.
	for _, result := range results {
		m.storeResult(result)
	}
}
