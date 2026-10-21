package gateway

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"sync"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

const latencySampleInterval = time.Minute
const pageProbeTimeout = 15 * time.Second

// LatencyMonitor measures the router's default route, not client routes.
// Only the chosen target is persisted; samples are ephemeral.
type LatencyMonitor struct {
	mu         sync.RWMutex
	snapshot   LatencySnapshot
	targets    []latencyTarget
	configured string
	revision   uint64
	// requested counts on-demand rounds asked for through Measure. A round
	// clears measuring only when no newer request arrived while it ran.
	requested uint64
	wake      chan struct{}
	probe     func(context.Context, string) (float64, error)
	dnsProbe  func(context.Context) (float64, error)
	Custom    *CustomLatencyMonitor
	Store     *core.Store
	Lock      *core.Lock
}
type latencyTarget struct{ Name, URL string }
type LatencySite struct {
	Name         string    `json:"name"`
	URL          string    `json:"url"`
	Milliseconds *float64  `json:"milliseconds"`
	CheckedAt    time.Time `json:"checked_at"`
	Selected     bool      `json:"selected"`
	Exit         string    `json:"exit,omitempty"`
	Icon         string    `json:"icon,omitempty"`
	Error        string    `json:"error,omitempty"`
}
type LatencySnapshot struct {
	State string `json:"state"`
	// Measuring reports an on-demand round in flight. It is false while the
	// once-a-minute background round runs, so passive readers never see a
	// spinner merely because the clock ticked.
	Measuring       bool          `json:"measuring"`
	DNSMilliseconds *float64      `json:"dns_milliseconds"`
	Milliseconds    *float64      `json:"milliseconds"`
	Target          string        `json:"target"`
	TargetURL       string        `json:"target_url"`
	CheckedAt       *time.Time    `json:"checked_at"`
	Sites           []LatencySite `json:"sites"`
	Custom          []LatencySite `json:"custom"`
}

func latencyTargets() []latencyTarget {
	return []latencyTarget{
		{Name: "Google", URL: "https://www.google.com/generate_204"},
		{Name: "Baidu", URL: "https://www.baidu.com/"},
		{Name: "Yandex", URL: "https://ya.ru/"},
		{Name: "Cloudflare", URL: "https://speed.cloudflare.com/__down?bytes=0"},
	}
}

func NewLatencyMonitor() *LatencyMonitor {
	m := &LatencyMonitor{targets: latencyTargets(), probe: probePage, dnsProbe: probeDNS, wake: make(chan struct{}, 1)}
	m.snapshot = m.emptySnapshot("")
	return m
}

// OpenLatencyMonitor restores the selection before the first request is served.
func OpenLatencyMonitor(store *core.Store, lock *core.Lock) (*LatencyMonitor, error) {
	m := NewLatencyMonitor()
	m.Store, m.Lock = store, lock
	target, err := m.loadTarget()
	if err != nil {
		return nil, err
	}
	m.snapshot = m.emptySnapshot(target)
	return m, nil
}

func (m *LatencyMonitor) emptySnapshot(target string) LatencySnapshot {
	s := LatencySnapshot{State: "measuring", Measuring: true, TargetURL: target, Sites: []LatencySite{}, Custom: []LatencySite{}}
	for _, t := range m.targets {
		selected := t.URL == target
		s.Sites = append(s.Sites, LatencySite{Name: t.Name, URL: t.URL, Selected: selected})
		if selected {
			s.Target = t.Name
		}
	}
	return s
}

func validLatencyTarget(target string) bool {
	if target == "" {
		return true
	}
	for _, t := range latencyTargets() {
		if t.URL == target {
			return true
		}
	}
	return false
}

// After startup, call with mu held. Writes also hold the global apply lock.
func (m *LatencyMonitor) loadTarget() (string, error) {
	if m.Store == nil {
		return m.configured, nil
	}
	doc, err := m.Store.Load()
	if err != nil {
		return "", err
	}
	cfg, err := FromDocument(doc)
	if err == nil && !validLatencyTarget(cfg.LatencyTarget) {
		err = fmt.Errorf("invalid Internet latency target")
	}
	return cfg.LatencyTarget, err
}
func (m *LatencyMonitor) saveTarget(target string) error {
	if m.Store == nil {
		m.configured = target
		return nil
	}
	doc, err := m.Store.Load()
	if err != nil {
		return err
	}
	cfg, err := FromDocument(doc)
	if err != nil {
		return err
	}
	cfg.LatencyTarget = target
	data, err := MarshalConfig(cfg)
	if err != nil {
		return err
	}
	doc.Set(ModuleName, data)
	return m.Store.Save(doc)
}

// SetTarget runs under the global apply lock. Empty requests a new selection.
func (m *LatencyMonitor) SetTarget(target string) error {
	if !validLatencyTarget(target) {
		return fmt.Errorf("choose a listed Internet target")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.saveTarget(target); err != nil {
		return err
	}
	m.revision++
	m.snapshot = m.emptySnapshot(target)
	select {
	case m.wake <- struct{}{}:
	default:
	}
	return nil
}

// A single HEAD exchange needs no document download or warm-up request.
// GotConn fires after DNS, TCP and TLS; the first byte must come from the
// authenticated remote HTTPS endpoint, even when a proxy accepts TCP locally.
func probePage(ctx context.Context, target string) (float64, error) {
	return probePageWithDial(ctx, target, nil)
}
func probePageMarked(ctx context.Context, target string, mark uint32) (float64, error) {
	return probePageWithDial(ctx, target, markedLatencyDial(mark))
}
func probePageWithDial(ctx context.Context, target string, dial func(context.Context, string, string) (net.Conn, error)) (float64, error) {
	transport := &http.Transport{DisableKeepAlives: true, TLSHandshakeTimeout: 4 * time.Second, DialContext: dial}
	defer transport.CloseIdleConnections()
	return probePageWithClient(ctx, target, &http.Client{Transport: transport})
}
func probePageWithClient(ctx context.Context, target string, client *http.Client) (float64, error) {
	origin, err := url.Parse(target)
	if err != nil || origin.Scheme != "https" || origin.Hostname() == "" {
		return 0, fmt.Errorf("invalid HTTPS target")
	}
	copyClient := *client
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	var start, first time.Time
	var traceMu sync.Mutex
	trace := &httptrace.ClientTrace{
		GotConn:              func(httptrace.GotConnInfo) { traceMu.Lock(); start = time.Now(); first = time.Time{}; traceMu.Unlock() },
		GotFirstResponseByte: func() { traceMu.Lock(); first = time.Now(); traceMu.Unlock() },
	}
	request, err := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, trace), http.MethodHead, target, nil)
	if err != nil {
		return 0, err
	}
	request.Header.Set("Cache-Control", "no-cache, no-store")
	response, err := copyClient.Do(request)
	if err != nil {
		return 0, err
	}
	response.Body.Close()
	traceMu.Lock()
	defer traceMu.Unlock()
	if start.IsZero() || first.IsZero() || first.Before(start) {
		return 0, fmt.Errorf("HTTPS timing unavailable")
	}
	return float64(first.Sub(start)) / float64(time.Millisecond), nil
}

func latencyError(err error) string {
	if err == nil {
		return ""
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return "DNS lookup failed"
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
	return float64(time.Since(start)) / float64(time.Millisecond), nil
}
func (m *LatencyMonitor) Snapshot() LatencySnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s := m.snapshot
	s.Sites = append([]LatencySite{}, s.Sites...)
	return s
}
func (m *LatencyMonitor) Run(ctx context.Context) {
	for ctx.Err() == nil {
		m.mu.Lock()
		requested, revision := m.requested, m.revision
		m.mu.Unlock()
		var custom sync.WaitGroup
		if m.Custom != nil {
			custom.Add(1)
			go func() { defer custom.Done(); m.Custom.sample(ctx) }()
		}
		m.sample(ctx)
		custom.Wait()
		// Only the round that answered the last on-demand request ends the
		// spinner. A click during a round leaves a wake token behind, so the
		// next loop iteration starts immediately and reports progress until
		// results newer than the click land. A target change sets its own
		// pending snapshot, so its new round does the clearing instead.
		m.mu.Lock()
		if m.requested == requested && m.revision == revision {
			m.snapshot.Measuring = false
		}
		m.mu.Unlock()
		timer := time.NewTimer(latencySampleInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-m.wake:
			timer.Stop()
		case <-timer.C:
		}
	}
	m.mu.Lock()
	m.snapshot.Measuring = false
	m.mu.Unlock()
}

// Measure starts a round now instead of waiting for the next one-minute tick.
// It returns without waiting: Measuring reports the round in progress, and the
// snapshot keeps its last completed values until the new ones are published.
func (m *LatencyMonitor) Measure() {
	m.mu.Lock()
	m.requested++
	m.snapshot.Measuring = true
	m.mu.Unlock()
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

// Only initial selection probes all candidates. Failure never changes a target.
func (m *LatencyMonitor) sample(ctx context.Context) {
	m.mu.Lock()
	target, err := m.loadTarget()
	revision := m.revision
	if err != nil {
		m.snapshot.State = "unavailable"
		m.snapshot.Milliseconds = nil
		m.snapshot.DNSMilliseconds = nil
	}
	m.mu.Unlock()
	if err != nil {
		return
	}
	s := m.emptySnapshot(target)
	var wg sync.WaitGroup
	for i, t := range m.targets {
		if target != "" && t.URL != target {
			continue
		}
		wg.Add(1)
		go func(i int, t latencyTarget) {
			defer wg.Done()
			bounded, cancel := context.WithTimeout(ctx, pageProbeTimeout)
			defer cancel()
			value, err := m.probe(bounded, t.URL)
			s.Sites[i].CheckedAt = time.Now().UTC()
			if err == nil {
				s.Sites[i].Milliseconds = &value
			} else {
				s.Sites[i].Error = latencyError(err)
			}
		}(i, t)
	}
	wg.Wait()
	if ctx.Err() != nil {
		return
	}
	dnsCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	if m.dnsProbe != nil {
		if value, err := m.dnsProbe(dnsCtx); err == nil {
			s.DNSMilliseconds = &value
		}
	}
	now := time.Now().UTC()
	s.CheckedAt = &now
	s.State = "unreachable"
	for _, site := range s.Sites {
		if site.Milliseconds != nil && (s.Milliseconds == nil || *site.Milliseconds < *s.Milliseconds) {
			s.Milliseconds = site.Milliseconds
			s.Target = site.Name
			s.TargetURL = site.URL
			s.State = "ok"
		}
	}
	publish := func() error {
		m.mu.Lock()
		defer m.mu.Unlock()
		current, err := m.loadTarget()
		if err != nil {
			return err
		}
		if current != target || revision != m.revision || ctx.Err() != nil {
			return nil
		}
		if target == "" && s.TargetURL != "" {
			if err := m.saveTarget(s.TargetURL); err != nil {
				return err
			}
		}
		for i := range s.Sites {
			s.Sites[i].Selected = s.Sites[i].URL == s.TargetURL
		}
		m.snapshot = s
		return nil
	}
	if m.Lock != nil {
		err = m.Lock.Do(ctx, publish)
	} else {
		err = publish()
	}
	if err != nil && ctx.Err() == nil {
		m.mu.Lock()
		if revision == m.revision {
			m.snapshot.State = "unavailable"
			m.snapshot.Milliseconds = nil
			m.snapshot.DNSMilliseconds = nil
		}
		m.mu.Unlock()
	}
}
