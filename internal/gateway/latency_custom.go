package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/runtimedb"
)

const maxCustomLatencySites = 20

type CustomLatencySite struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Exit string `json:"exit,omitempty"`
	Icon string `json:"icon,omitempty"`
}

// monitorSite is one probe target: a catalog site with its override, or a site
// the operator added by hand.
type monitorSite struct {
	Name     string
	URL      string
	Exit     string
	Icon     string
	Category string
	Builtin  bool
}

// Custom targets are independent of the Internet candidate selection.
// Intent belongs to configuration; hourly measurements belong to runtime.db.
type CustomLatencyMonitor struct {
	mu           sync.RWMutex
	db           *runtimedb.Store
	dirty        []runtimedb.LatencySample
	now          func() time.Time
	wait         func(context.Context, time.Duration) bool
	Log          *slog.Logger
	path         string
	store        *core.Store
	sites        []CustomLatencySite
	common       []CommonLatencySite
	commonExits  map[string]string
	pins         []string
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
	// Sites whose names are now catalog sites move to overrides: the catalog
	// owns their URL and category, and only the exit remains operator intent.
	commonExits := map[string]string{}
	for _, entry := range cfg.LatencyCommon {
		if _, ok := commonLatencySite(entry.Name); ok && entry.Exit != "" {
			commonExits[entry.Name] = entry.Exit
		}
	}
	custom := make([]CustomLatencySite, 0, len(cfg.LatencySites))
	for _, site := range cfg.LatencySites {
		if _, ok := commonLatencySite(site.Name); ok {
			if _, exists := commonExits[site.Name]; !exists && site.Exit != "" {
				commonExits[site.Name] = site.Exit
			}
			continue
		}
		custom = append(custom, site)
	}
	nextCommon := make([]CommonLatency, 0, len(commonExits))
	for _, site := range latencyCatalog() {
		if exit := commonExits[site.Name]; exit != "" {
			nextCommon = append(nextCommon, CommonLatency{Name: site.Name, Exit: exit})
		}
	}
	if len(custom) != len(cfg.LatencySites) || !sameCommon(nextCommon, cfg.LatencyCommon) {
		cfg.LatencySites = custom
		cfg.LatencyCommon = nextCommon
		raw, err := MarshalConfig(cfg)
		if err != nil {
			return nil, err
		}
		doc.Set(ModuleName, raw)
		if err := store.Save(doc); err != nil {
			return nil, err
		}
	}
	m := &CustomLatencyMonitor{
		store: store, probe: probePage, probeThrough: probePageMarked,
		results: []LatencySite{}, inflight: map[string]bool{},
		common: latencyCatalog(), commonExits: commonExits, pins: cfg.LatencyPins,
	}
	m.sites = custom
	m.resetResults()
	return m, nil
}

func sameCommon(a, b []CommonLatency) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
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
		u, err := url.Parse(site.URL)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.Opaque != "" || u.Port() != "" {
			return fmt.Errorf("%s: use an HTTPS URL without credentials, port, or fragment", name)
		}
	}
	return nil
}

// allSitesLocked returns the catalog sites followed by the operator's sites.
// Callers hold m.mu (or run before the monitor is shared).
func (m *CustomLatencyMonitor) allSitesLocked() []monitorSite {
	sites := make([]monitorSite, 0, len(m.common)+len(m.sites))
	for _, site := range m.common {
		sites = append(sites, monitorSite{Name: site.Name, URL: site.URL, Exit: m.commonExits[site.Name], Icon: site.Icon, Category: site.Category, Builtin: true})
	}
	for _, site := range m.sites {
		sites = append(sites, monitorSite{Name: site.Name, URL: site.URL, Exit: site.Exit, Icon: site.Icon})
	}
	return sites
}

func (m *CustomLatencyMonitor) resetResults() {
	previous := make(map[string]LatencySite, len(m.results))
	for _, result := range m.results {
		previous[result.URL+"\x00"+result.Exit] = result
	}
	sites := m.allSitesLocked()
	m.results = make([]LatencySite, 0, len(sites))
	for _, site := range sites {
		result := LatencySite{Name: site.Name, URL: site.URL, Exit: site.Exit, Icon: site.Icon, Category: site.Category, Builtin: site.Builtin}
		if old, ok := previous[site.URL+"\x00"+site.Exit]; ok {
			result.Milliseconds, result.CheckedAt, result.Error = old.Milliseconds, old.CheckedAt, old.Error
			result.Samples = append([]LatencySample{}, old.Samples...)
		}
		// A reorder or rename during an on-demand probe keeps its spinner on the
		// row that still names the same target.
		result.Measuring = m.inflight[site.URL+"\x00"+site.Exit]
		m.results = append(m.results, result)
	}
}

// setContext makes on-demand probes follow the daemon.
func (m *CustomLatencyMonitor) setContext(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.base = ctx
}

// MeasureAll starts one on-demand probe for every site in category, or for
// every site when category is empty. "custom" selects the operator's sites.
func (m *CustomLatencyMonitor) MeasureAll(category string) error {
	m.refreshConfig()
	m.mu.RLock()
	names := make([]string, 0, len(m.common)+len(m.sites))
	for _, site := range m.allSitesLocked() {
		switch {
		case category == "":
		case category == "custom":
			if site.Builtin {
				continue
			}
		case !site.Builtin || site.Category != category:
			continue
		}
		names = append(names, site.Name)
	}
	m.mu.RUnlock()
	for _, name := range names {
		if err := m.Measure(name); err != nil {
			return err
		}
	}
	return nil
}

// Measure probes one stored site now and marks only that row as measuring.
//
// It returns without waiting: the result replaces the stored one when it
// arrives. A second click while the same target is probing is a no-op, so a
// slow site cannot accumulate parallel probes.
func (m *CustomLatencyMonitor) Measure(name string) error {
	m.refreshConfig()
	m.mu.Lock()
	var site monitorSite
	found := false
	for _, candidate := range m.allSitesLocked() {
		if candidate.Name == name {
			site, found = candidate, true
			break
		}
	}
	if !found {
		m.mu.Unlock()
		return fmt.Errorf("no monitored site named %q", name)
	}
	key := site.URL + "\x00" + site.Exit
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
	started := m.clock()
	go m.probeOne(base, site, started)
	return nil
}

// probeOne runs one on-demand probe and publishes its outcome.
func (m *CustomLatencyMonitor) probeOne(ctx context.Context, site monitorSite, started time.Time) {
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
	result := LatencySite{Name: site.Name, URL: site.URL, Exit: site.Exit, Icon: site.Icon, Category: site.Category, Builtin: site.Builtin, CheckedAt: started.UTC()}
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

// storeResult merges by target/hour, ordered by attempt start rather than finish.
// Callers hold m.mu. An old hour may arrive late without replacing the live value.
func (m *CustomLatencyMonitor) storeResult(result LatencySite) {
	key := result.URL + "\x00" + result.Exit
	for i, site := range m.allSitesLocked() {
		if site.URL != result.URL || site.Exit != result.Exit || i >= len(m.results) {
			continue
		}
		current := m.results[i]
		sample := LatencySample{Milliseconds: result.Milliseconds, Error: result.Error, CheckedAt: result.CheckedAt, HourStart: latencyHour(result.CheckedAt.In(time.Local))}
		samples := append([]LatencySample{}, current.Samples...)
		replaced := false
		accepted := true
		for j := range samples {
			if samples[j].HourStart.Equal(sample.HourStart) {
				replaced = true
				if samples[j].CheckedAt.After(sample.CheckedAt) {
					accepted = false
				} else {
					samples[j] = sample
				}
				break
			}
		}
		if !replaced {
			samples = append(samples, sample)
		}
		if accepted && m.db != nil {
			m.dirty = append(m.dirty, runtimedb.LatencySample{URL: result.URL, Exit: result.Exit, HourStart: sample.HourStart, CheckedAt: sample.CheckedAt, Milliseconds: sample.Milliseconds, Error: sample.Error})
		}
		sort.Slice(samples, func(a, b int) bool { return samples[a].HourStart.Before(samples[b].HourStart) })
		samples = retainedLatencySamples(samples, m.clock())
		if !current.CheckedAt.After(result.CheckedAt) {
			current.Milliseconds, current.Error, current.CheckedAt = result.Milliseconds, result.Error, result.CheckedAt
		}
		current.Samples = samples
		current.Measuring = m.inflight[key]
		m.results[i] = current
	}
	m.saveHistoryLocked()
}

func (m *CustomLatencyMonitor) Snapshot() []LatencySite {
	m.refreshConfig()
	m.mu.Lock()
	defer m.mu.Unlock()
	out := append([]LatencySite{}, m.results...)
	for i := range out {
		out[i].Pinned = slices.Contains(m.pins, out[i].Name)
		m.results[i].Samples = retainedLatencySamples(m.results[i].Samples, m.clock())
		out[i].Samples = append([]LatencySample{}, m.results[i].Samples...)
	}
	return out
}

func (m *CustomLatencyMonitor) refreshConfig() {
	if m.store == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	doc, err := m.store.Load()
	if err != nil {
		return
	}
	cfg, err := FromDocument(doc)
	if err != nil {
		return
	}
	exits := map[string]string{}
	for _, entry := range cfg.LatencyCommon {
		if _, ok := commonLatencySite(entry.Name); ok && entry.Exit != "" {
			exits[entry.Name] = entry.Exit
		}
	}
	m.pins = append([]string{}, cfg.LatencyPins...)
	old, _ := json.Marshal(m.sites)
	next, _ := json.Marshal(cfg.LatencySites)
	if string(old) != string(next) || !reflect.DeepEqual(m.commonExits, exits) {
		m.sites = append([]CustomLatencySite{}, cfg.LatencySites...)
		m.commonExits = exits
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

// Replace stores the operator's list. Entries that name a catalog site update
// that site's exit and are not stored as custom sites; everything else is the
// custom list. Omitting a catalog site never removes it.
func (m *CustomLatencyMonitor) Replace(sites []CustomLatencySite) error {
	m.mu.RLock()
	nextExits := make(map[string]string, len(m.commonExits))
	for name, exit := range m.commonExits {
		nextExits[name] = exit
	}
	m.mu.RUnlock()
	custom := make([]CustomLatencySite, 0, len(sites))
	splitCatalog := m.store != nil || m.common != nil
	for _, site := range sites {
		if splitCatalog {
			if _, ok := commonLatencySite(site.Name); ok {
				nextExits[site.Name] = site.Exit
				continue
			}
		}
		custom = append(custom, site)
	}
	if err := validateCustomSites(custom); err != nil {
		return err
	}
	for _, site := range sites {
		if site.Exit == "" {
			continue
		}
		if m.ValidateExit == nil {
			return errors.New("gateway exit validation unavailable")
		}
		if err := m.ValidateExit(site.Exit); err != nil {
			return err
		}
	}
	common := make([]CommonLatency, 0, len(nextExits))
	for _, site := range latencyCatalog() {
		if exit := nextExits[site.Name]; exit != "" {
			common = append(common, CommonLatency{Name: site.Name, Exit: exit})
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
		cfg.LatencySites = append([]CustomLatencySite{}, custom...)
		cfg.LatencyCommon = common
		data, err := MarshalConfig(cfg)
		if err != nil {
			return err
		}
		doc.Set(ModuleName, data)
		if err := m.store.Save(doc); err != nil {
			return err
		}
		m.sites = append([]CustomLatencySite{}, custom...)
		m.commonExits = nextExits
		m.resetResults()
		m.icons = nil
		return nil
	}
	data, err := json.MarshalIndent(custom, "", "  ")
	if err != nil {
		return err
	}
	if err := core.WriteFileAtomic(m.path, append(data, '\n'), 0o644); err != nil {
		return err
	}
	m.sites = append([]CustomLatencySite{}, custom...)
	m.commonExits = nextExits
	m.resetResults()
	m.icons = nil
	return nil
}

func (m *CustomLatencyMonitor) sample(ctx context.Context) {
	m.sampleAt(ctx, m.clock(), false)
}

// missingOnly is startup catch-up: a restored or manually measured hour stays intact.
func (m *CustomLatencyMonitor) sampleAt(ctx context.Context, started time.Time, missingOnly bool) {
	m.refreshConfig()
	m.mu.RLock()
	sites := m.allSitesLocked()
	if missingOnly {
		pending := sites[:0]
		hour := latencyHour(started.In(time.Local))
		for i, site := range sites {
			measured := false
			if i < len(m.results) {
				for _, sample := range m.results[i].Samples {
					if sample.HourStart.Equal(hour) {
						measured = true
						break
					}
				}
			}
			if !measured && !m.inflight[site.URL+"\x00"+site.Exit] {
				pending = append(pending, site)
			}
		}
		sites = pending
	}
	m.mu.RUnlock()
	var wg sync.WaitGroup
	for _, site := range sites {
		wg.Go(func() { m.probeBackground(ctx, site, started) })
	}
	wg.Wait()
}

func (m *CustomLatencyMonitor) probeBackground(ctx context.Context, site monitorSite, started time.Time) {
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
	if ctx.Err() != nil {
		return
	}
	result := LatencySite{URL: site.URL, Exit: site.Exit, CheckedAt: started.UTC()}
	if err == nil {
		result.Milliseconds = &value
	} else if site.Exit != "" && strings.Contains(err.Error(), "gateway exit") {
		result.Error = "Gateway exit unavailable"
	} else {
		result.Error = latencyError(err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if ctx.Err() != nil {
		return
	}
	m.storeResult(result)
}
