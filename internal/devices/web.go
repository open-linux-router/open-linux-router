package devices

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/webicon"
)

const WebProbeEvery = 24 * time.Hour
const webScanEvery = 10 * time.Minute
const webScanBatch = 16

type WebSite struct {
	MAC       string    `json:"mac"`
	IP        string    `json:"ip"`
	URL       string    `json:"url"`
	CheckedAt time.Time `json:"checked_at"`
}

type webResult struct {
	WebSite
	Icon string `json:"icon,omitempty"`
	Kind string `json:"kind,omitempty"`
}

type webFile struct {
	Version int                  `json:"version"`
	Results map[string]webResult `json:"results"`
}

// WebDiscovery keeps observations separate from operator configuration. Missing
// pages are cached too, so quiet devices are not retried on every page visit.
type WebDiscovery struct {
	Path    string
	Client  *http.Client
	Log     *slog.Logger
	mu      sync.RWMutex
	results map[string]webResult
}

func WebPath(root string) string {
	return filepath.Join(root, "/var/lib/open-linux-router/devices/web.json")
}

func NewWebDiscovery(path string) *WebDiscovery {
	d := &WebDiscovery{Path: path, results: map[string]webResult{}}
	data, err := os.ReadFile(path)
	if err == nil {
		var file webFile
		if json.Unmarshal(data, &file) == nil && file.Version == 1 && file.Results != nil {
			d.results = file.Results
		}
	}
	return d
}

func webAddress(r Resolved) string {
	if !r.Online() || r.Presence == nil {
		return ""
	}
	for _, raw := range r.Presence.IPs {
		ip, err := netip.ParseAddr(raw)
		if err == nil && ip.Is4() && ip.IsPrivate() {
			return ip.String()
		}
	}
	return ""
}

func (d *WebDiscovery) Sites() []WebSite {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]WebSite, 0, len(d.results))
	for _, entry := range d.results {
		if entry.URL != "" {
			out = append(out, entry.WebSite)
		}
	}
	slices.SortFunc(out, func(a, b WebSite) int { return strings.Compare(a.MAC, b.MAC) })
	return out
}

func (d *WebDiscovery) Icon(mac string) ([]byte, string) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	entry := d.results[mac]
	data, err := base64.StdEncoding.DecodeString(entry.Icon)
	if err != nil {
		return nil, ""
	}
	return data, entry.Kind
}

func (d *WebDiscovery) Run(ctx context.Context, list func(context.Context) ([]Resolved, error)) {
	tick := time.NewTicker(webScanEvery)
	defer tick.Stop()
	d.scan(ctx, list)
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			d.scan(ctx, list)
		}
	}
}

func (d *WebDiscovery) scan(ctx context.Context, list func(context.Context) ([]Resolved, error)) {
	devices, err := list(ctx)
	if err != nil {
		return
	}
	changed := false
	now := time.Now()
	owners := map[string]int{}
	for _, device := range devices {
		if ip := webAddress(device); ip != "" {
			owners[ip]++
		}
	}
	probed := 0
	for _, device := range devices {
		if ctx.Err() != nil {
			return
		}
		ip := webAddress(device)
		if ip == "" || owners[ip] != 1 {
			d.mu.Lock()
			if _, ok := d.results[device.MAC]; ok {
				delete(d.results, device.MAC)
				changed = true
			}
			d.mu.Unlock()
			continue
		}
		d.mu.RLock()
		previous, exists := d.results[device.MAC]
		d.mu.RUnlock()
		if exists && previous.IP == ip && now.Sub(previous.CheckedAt) < WebProbeEvery {
			continue
		}
		if exists && previous.IP != ip {
			d.mu.Lock()
			delete(d.results, device.MAC)
			d.mu.Unlock()
			changed = true
		}
		if probed >= webScanBatch {
			continue
		}
		probed++
		entry := webResult{WebSite: WebSite{MAC: device.MAC, IP: ip, CheckedAt: now}}
		for _, scheme := range []string{"http", "https"} {
			origin := fmt.Sprintf("%s://%s", scheme, ip)
			if !probeWeb(ctx, d.Client, origin) {
				continue
			}
			entry.URL = origin
			data, kind, _ := webicon.Discover(ctx, d.Client, origin)
			entry.Icon, entry.Kind = base64.StdEncoding.EncodeToString(data), kind
			break
		}
		d.mu.Lock()
		d.results[device.MAC] = entry
		d.mu.Unlock()
		changed = true
	}
	// Forget entries no longer in the inventory, including devices with a new MAC.
	known := make(map[string]bool, len(devices))
	for _, device := range devices {
		known[device.MAC] = true
	}
	d.mu.Lock()
	for mac := range d.results {
		if !known[mac] {
			delete(d.results, mac)
			changed = true
		}
	}
	d.mu.Unlock()
	if changed && d.Path != "" {
		d.mu.RLock()
		data, err := json.Marshal(webFile{Version: 1, Results: d.results})
		d.mu.RUnlock()
		if err == nil {
			if err = core.WriteFileAtomic(d.Path, data, 0o600); err != nil && d.Log != nil {
				d.Log.Warn("could not save discovered web pages", "error", err)
			}
		}
	}
}

func probeWeb(ctx context.Context, client *http.Client, origin string) bool {
	if client == nil {
		client = http.DefaultClient
	}
	copyClient := *client
	copyClient.Timeout = 2 * time.Second
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, origin, nil)
	if err != nil {
		return false
	}
	resp, err := copyClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return (resp.StatusCode >= 200 && resp.StatusCode < 400) || resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden
}

// A bounded dialer prevents a discovered page from leading the probe to a
// different address via DNS or a proxy. IPs here come only from device presence.
func WebClient() *http.Client {
	// Private-IP management pages often use self-signed certificates. The IP is
	// pinned by the dialer below; certificate validation is not meaningful here.
	transport := &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ip, err := netip.ParseAddr(host)
		if err != nil || !ip.Is4() || !ip.IsPrivate() {
			return nil, fmt.Errorf("web probe requires a private IPv4 address")
		}
		return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, network, address)
	}, TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	return &http.Client{Transport: transport, Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
