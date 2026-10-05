package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

const maxCustomLatencySites = 12

type CustomLatencySite struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// Custom targets are independent of the Internet candidate selection.
// Their intent survives restarts, while measurements remain ephemeral.
type CustomLatencyMonitor struct {
	mu      sync.RWMutex
	path    string
	sites   []CustomLatencySite
	results []LatencySite
	probe   func(context.Context, string) (float64, error)
}

func NewCustomLatencyMonitor(path string) (*CustomLatencyMonitor, error) {
	m := &CustomLatencyMonitor{path: path, probe: probeHTTPS, results: []LatencySite{}}
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

func (m *CustomLatencyMonitor) resetResults() {
	m.results = make([]LatencySite, len(m.sites))
	for i, site := range m.sites {
		m.results[i] = LatencySite{Name: site.Name, URL: site.URL}
	}
}

func (m *CustomLatencyMonitor) Snapshot() []LatencySite {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]LatencySite{}, m.results...)
}

func (m *CustomLatencyMonitor) Config() []CustomLatencySite {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]CustomLatencySite{}, m.sites...)
}

func (m *CustomLatencyMonitor) Replace(sites []CustomLatencySite) error {
	if err := validateCustomSites(sites); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	data, err := json.MarshalIndent(sites, "", "  ")
	if err != nil {
		return err
	}
	if err := core.WriteFileAtomic(m.path, append(data, '\n'), 0o644); err != nil {
		return err
	}
	m.sites = append([]CustomLatencySite{}, sites...)
	m.resetResults()
	return nil
}

func (m *CustomLatencyMonitor) sample(ctx context.Context) {
	sites := m.Config()
	results := make([]LatencySite, len(sites))
	var wg sync.WaitGroup
	for i, site := range sites {
		wg.Add(1)
		go func(i int, site CustomLatencySite) {
			defer wg.Done()
			bounded, cancel := context.WithTimeout(ctx, 4*time.Second)
			defer cancel()
			value, err := m.probe(bounded, site.URL)
			results[i] = LatencySite{Name: site.Name, URL: site.URL, CheckedAt: time.Now().UTC()}
			if err == nil {
				results[i].Milliseconds = &value
			}
		}(i, site)
	}
	wg.Wait()
	if ctx.Err() != nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	// A settings edit during a probe must never publish measurements for removed targets.
	if len(m.sites) != len(sites) {
		return
	}
	for i := range sites {
		if m.sites[i] != sites[i] {
			return
		}
	}
	m.results = results
}
