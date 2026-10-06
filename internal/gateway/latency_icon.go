package gateway

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/ingress"
)

type latencyIcon struct {
	data    []byte
	kind    string
	checked time.Time
}

// Icon discovers a monitored site's favicon through the same selected exit.
// The result is cached independently of probe measurements.
func (m *CustomLatencyMonitor) Icon(ctx context.Context, name string) ([]byte, string, error) {
	m.mu.RLock()
	var site CustomLatencySite
	for _, candidate := range m.sites {
		if candidate.Name == name {
			site = candidate
			break
		}
	}
	cached, ok := m.icons[name]
	m.mu.RUnlock()
	if site.Name == "" {
		return nil, "", errors.New("site not found")
	}
	if ok && time.Since(cached.checked) < 24*time.Hour {
		return cached.data, cached.kind, nil
	}
	if m.iconClient != nil {
		client, err := m.iconClient(&site)
		if err != nil {
			return nil, "", err
		}
		return m.discoverIcon(ctx, site, client)
	}
	transport := &http.Transport{DisableKeepAlives: true, TLSHandshakeTimeout: 3 * time.Second}
	defer transport.CloseIdleConnections()
	if site.Exit != "" {
		if m.Resolve == nil {
			return nil, "", errors.New("gateway exit unavailable")
		}
		mark, err := m.Resolve(site.Exit)
		if err != nil {
			return nil, "", err
		}
		transport.DialContext = markedLatencyDial(mark)
	}
	return m.discoverIcon(ctx, site, &http.Client{Transport: transport})
}

func (m *CustomLatencyMonitor) discoverIcon(ctx context.Context, site CustomLatencySite, client *http.Client) ([]byte, string, error) {
	target, _ := url.Parse(site.URL)
	target.RawQuery, target.Fragment = "", ""
	data, kind, err := ingress.DiscoverIcon(ctx, client, target.String())
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, current := range m.sites {
		if current == site {
			if err == nil {
				if m.icons == nil {
					m.icons = map[string]latencyIcon{}
				}
				m.icons[site.Name] = latencyIcon{data: data, kind: kind, checked: time.Now()}
			}
			break
		}
	}
	return data, kind, err
}
