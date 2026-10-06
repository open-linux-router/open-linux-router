package gateway

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	latencySampleInterval      = time.Minute
	latencyDiscoveryInterval   = 15 * time.Second
	latencyRediscoveryInterval = 24 * time.Hour
)

// LatencyMonitor measures the router's default route, not individual client
// routes. Samples and selection are ephemeral and relearned after restart.
type LatencyMonitor struct {
	mu               sync.RWMutex
	snapshot         LatencySnapshot
	targets          []latencyTarget
	selected         []int
	discoveryStarted time.Time
	rounds           int
	probe            func(context.Context, string) (float64, error)
	dnsProbe         func(context.Context) (float64, error)
	Custom           *CustomLatencyMonitor
}
type latencyTarget struct {
	Name, URL string
	samples   []float64
}
type LatencySite struct {
	Name         string    `json:"name"`
	URL          string    `json:"url"`
	Milliseconds *float64  `json:"milliseconds"`
	CheckedAt    time.Time `json:"checked_at"`
	Selected     bool      `json:"selected"`
	Exit         string    `json:"exit,omitempty"`
	Error        string    `json:"error,omitempty"`
}
type LatencySnapshot struct {
	State           string        `json:"state"`
	DNSMilliseconds *float64      `json:"dns_milliseconds"`
	Milliseconds    *float64      `json:"milliseconds"`
	Target          string        `json:"target"`
	CheckedAt       *time.Time    `json:"checked_at"`
	Sites           []LatencySite `json:"sites"`
	Custom          []LatencySite `json:"custom"`
}

func NewLatencyMonitor() *LatencyMonitor {
	return &LatencyMonitor{targets: []latencyTarget{
		{Name: "Google", URL: "https://www.google.com/"},
		{Name: "Baidu", URL: "https://www.baidu.com/"},
		{Name: "Yandex", URL: "https://ya.ru/"},
		{Name: "Cloudflare", URL: "https://www.cloudflare.com/"},
	}, probe: probePage, dnsProbe: probeDNS, snapshot: LatencySnapshot{State: "measuring", Sites: []LatencySite{}, Custom: []LatencySite{}}}
}

const (
	pageProbeTimeout = 15 * time.Second
	pageProbeLimit   = 2 << 20
)

// probePage measures the whole HTTPS document, including DNS, redirects and body.
// Assets and JavaScript are deliberately not fetched.
func probePage(ctx context.Context, target string) (float64, error) {
	return probePageWithDial(ctx, target, nil)
}

func probePageMarked(ctx context.Context, target string, mark uint32) (float64, error) {
	return probePageWithDial(ctx, target, markedLatencyDial(mark))
}

func probePageWithDial(ctx context.Context, target string, dial func(context.Context, string, string) (net.Conn, error)) (float64, error) {
	transport := &http.Transport{DisableKeepAlives: true, TLSHandshakeTimeout: 4 * time.Second, DialContext: dial}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	return probePageWithClient(ctx, target, client)
}

func probePageWithClient(ctx context.Context, target string, client *http.Client) (float64, error) {
	origin, err := url.Parse(target)
	if err != nil || origin.Scheme != "https" || origin.Hostname() == "" {
		return 0, fmt.Errorf("invalid HTTPS target")
	}
	copyClient := *client
	copyClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || req.URL.Scheme != "https" || !strings.EqualFold(req.URL.Hostname(), origin.Hostname()) || req.URL.Port() != origin.Port() {
			return http.ErrUseLastResponse
		}
		return nil
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return 0, err
	}
	request.Header.Set("Accept", "text/html")
	start := time.Now()
	response, err := copyClient.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return 0, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	if kind := response.Header.Get("Content-Type"); kind != "" && !strings.HasPrefix(strings.ToLower(kind), "text/html") {
		return 0, fmt.Errorf("not an HTML page")
	}
	count, err := io.Copy(io.Discard, io.LimitReader(response.Body, pageProbeLimit+1))
	if err != nil {
		return 0, err
	}
	if count > pageProbeLimit {
		return 0, fmt.Errorf("page exceeds 2 MiB")
	}
	return float64(time.Since(start).Microseconds()) / 1000, nil
}

func latencyError(err error) string {
	if err == nil {
		return ""
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return "DNS lookup failed"
	}
	if strings.HasPrefix(err.Error(), "HTTP ") || err.Error() == "page exceeds 2 MiB" || err.Error() == "not an HTML page" {
		return err.Error()
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "Timed out"
	}
	var network net.Error
	if errors.As(err, &network) && network.Timeout() {
		return "Timed out"
	}
	return "Connection failed"
}

func probeDNS(ctx context.Context) (float64, error) {
	start := time.Now()
	_, err := net.DefaultResolver.LookupHost(ctx, "example.com")
	if err != nil {
		return 0, err
	}
	return float64(time.Since(start).Microseconds()) / 1000, nil
}

func (m *LatencyMonitor) Snapshot() LatencySnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s := m.snapshot
	s.Sites = append([]LatencySite{}, s.Sites...)
	return s
}
func (m *LatencyMonitor) Run(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		var custom sync.WaitGroup
		if m.Custom != nil {
			custom.Add(1)
			go func() { defer custom.Done(); m.Custom.sample(ctx) }()
		}
		m.sample(ctx)
		custom.Wait()
		timer := time.NewTimer(m.nextInterval())
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// Only discovery bursts are frequent. If no site qualifies after four rounds,
// back off too, so an outage never becomes continuous probing.
func (m *LatencyMonitor) nextInterval() time.Duration {
	if len(m.selected) == 0 && m.rounds > 0 {
		return latencyDiscoveryInterval
	}
	return latencySampleInterval
}

// sample has one writer (Run). Readers only see complete rounds through Snapshot.
func (m *LatencyMonitor) sample(ctx context.Context) {
	// Relearn every day; an outage immediately restarts discovery.
	if m.discoveryStarted.IsZero() || time.Since(m.discoveryStarted) >= latencyRediscoveryInterval {
		m.discoveryStarted = time.Now()
		m.rounds = 0
		m.selected = nil
		for i := range m.targets {
			m.targets[i].samples = nil
		}
	}
	indices := append([]int(nil), m.selected...)
	if len(indices) == 0 {
		for i := range m.targets {
			indices = append(indices, i)
		}
	}
	sites := m.Snapshot().Sites
	if len(sites) == 0 {
		for _, t := range m.targets {
			sites = append(sites, LatencySite{Name: t.Name, URL: t.URL})
		}
	}
	var wg sync.WaitGroup
	for _, i := range indices {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			bounded, cancel := context.WithTimeout(ctx, pageProbeTimeout)
			defer cancel()
			value, err := m.probe(bounded, m.targets[i].URL)
			sites[i].Milliseconds = nil
			sites[i].CheckedAt = time.Now().UTC()
			if err == nil {
				sites[i].Milliseconds = &value
			}
		}(i)
	}
	wg.Wait()
	if ctx.Err() != nil {
		return
	}
	dnsCtx, dnsCancel := context.WithTimeout(ctx, 4*time.Second)
	defer dnsCancel()
	var dnsMilliseconds *float64
	if m.dnsProbe != nil {
		value, err := m.dnsProbe(dnsCtx)
		if err == nil {
			dnsMilliseconds = &value
		}
	}
	now := time.Now().UTC()
	s := LatencySnapshot{State: "unreachable", CheckedAt: &now, Sites: sites, DNSMilliseconds: dnsMilliseconds}
	failed := false
	for _, i := range indices {
		v := sites[i].Milliseconds
		if v == nil {
			failed = true
			continue
		}
		m.targets[i].samples = append(m.targets[i].samples, *v)
		if s.Milliseconds == nil || *v < *s.Milliseconds {
			s.Milliseconds = v
			s.Target = sites[i].Name
			s.State = "ok"
		}
	}
	m.rounds++
	if len(m.selected) > 0 && failed {
		m.selected = nil
		m.rounds = 0
		for i := range m.targets {
			m.targets[i].samples = nil
		}
	} else if len(m.selected) == 0 && m.rounds >= 4 {
		// Require three successes in the four discovery rounds. Median selection
		// avoids choosing a site on one lucky response; the displayed value remains
		// the minimum successful measurement in the current round.
		candidates := []int{}
		median := func(i int) float64 {
			v := append([]float64(nil), m.targets[i].samples...)
			sort.Float64s(v)
			return v[len(v)/2]
		}
		for i := range m.targets {
			if len(m.targets[i].samples) >= 3 && sites[i].Milliseconds != nil {
				candidates = append(candidates, i)
			}
		}
		sort.SliceStable(candidates, func(i, j int) bool { return median(candidates[i]) < median(candidates[j]) })
		if len(candidates) > 0 {
			m.selected = append(m.selected, candidates[0])
			if len(candidates) > 1 && median(candidates[1]) <= median(candidates[0])*1.2 {
				m.selected = append(m.selected, candidates[1])
			}
		}
		if len(m.selected) == 0 {
			m.rounds = 0
			for i := range m.targets {
				m.targets[i].samples = nil
			}
		}
	}
	for i := range s.Sites {
		s.Sites[i].Selected = false
		for _, j := range m.selected {
			if i == j {
				s.Sites[i].Selected = true
			}
		}
	}
	m.mu.Lock()
	m.snapshot = s
	m.mu.Unlock()
}
