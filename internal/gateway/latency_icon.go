package gateway

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/ingress"
)

func validateLatencyIcon(icon string) error { return ingress.ValidateCustomIcon(icon) }

type latencyIcon struct {
	data    []byte
	kind    string
	checked time.Time
}

// Icon resolves a monitored site's selected icon locally, or discovers its favicon
// through the same selected exit. The result is cached independently of probes.
func (m *CustomLatencyMonitor) Icon(ctx context.Context, name string) ([]byte, string, error) {
	m.refreshConfig()
	m.mu.RLock()
	var site CustomLatencySite
	for _, candidate := range m.allSitesLocked() {
		if candidate.Name == name {
			site = CustomLatencySite{Name: candidate.Name, URL: candidate.URL, Exit: candidate.Exit, Icon: candidate.Icon}
			break
		}
	}
	cached, ok := m.icons[name]
	m.mu.RUnlock()
	if site.Name == "" {
		return nil, "", errors.New("site not found")
	}
	if strings.HasPrefix(site.Icon, "data:") {
		kind := strings.SplitN(strings.TrimPrefix(site.Icon, "data:"), ";", 2)[0]
		data, _ := base64.StdEncoding.DecodeString(strings.SplitN(site.Icon, ",", 2)[1])
		return data, kind, nil
	}
	if ok && time.Since(cached.checked) < 24*time.Hour && !strings.HasPrefix(site.Icon, "data:") {
		return cached.data, cached.kind, nil
	}
	if strings.HasPrefix(site.Icon, "thesvg:") {
		data, kind, err := ingress.CustomIcon(site.Icon)
		if err != nil {
			return nil, "", err
		}
		m.mu.Lock()
		if m.icons == nil {
			m.icons = map[string]latencyIcon{}
		}
		if current := m.siteByName(name); current == site {
			m.icons[name] = latencyIcon{data: data, kind: "image/svg+xml", checked: time.Now()}
		}
		m.mu.Unlock()
		return data, kind, nil
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

// siteByName is called with m.mu held.
func (m *CustomLatencyMonitor) siteByName(name string) CustomLatencySite {
	for _, site := range m.allSitesLocked() {
		if site.Name == name {
			return CustomLatencySite{Name: site.Name, URL: site.URL, Exit: site.Exit, Icon: site.Icon}
		}
	}
	return CustomLatencySite{}
}
