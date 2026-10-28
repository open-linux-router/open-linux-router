package tools

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/geoip"
)

// splitBudget bounds the whole check. Every probe is one small request; a
// provider that needs longer is reported as unavailable rather than awaited.
const splitBudget = 12 * time.Second

// SplitProbe is one public endpoint that reports the address it sees. The
// catalog is fixed and short on purpose: a provider belongs here only when it
// answers with the caller's address and stays reachable without an account.
type splitProbe struct {
	name   string
	region string
	url    string
	read   func(*http.Response) (string, error)
}

// splitProbes are the vantage points this check can offer.
//
// They are chosen for independence rather than for count: one per operator or
// platform, so two answers that agree are evidence and not a mirror. A probe
// that starts refusing automated requests shows up as an error in the report
// instead of silently disappearing from the denominator.
var splitProbes = []splitProbe{
	{name: "ipify", region: "Global", url: "https://api.ipify.org", read: readBareAddress},
	{name: "ipinfo", region: "Global", url: "https://ipinfo.io/ip", read: readBareAddress},
	{name: "icanhazip", region: "Global", url: "https://icanhazip.com", read: readBareAddress},
	{name: "ifconfig.me", region: "Global", url: "https://ifconfig.me/ip", read: readBareAddress},
	{name: "AWS checkip", region: "Global", url: "https://checkip.amazonaws.com", read: readBareAddress},
	{name: "Cloudflare", region: "Global", url: "https://www.cloudflare.com/cdn-cgi/trace", read: readTraceIP},
	{name: "ipip.net", region: "China", url: "https://myip.ipip.net", read: readAddressInText},
}

// SplitProbeResult is what one endpoint reported, or why it reported nothing.
type SplitProbeResult struct {
	Probe   string       `json:"probe"`
	Region  string       `json:"region"`
	IP      string       `json:"ip,omitempty"`
	Place   *geoip.Place `json:"place,omitempty"`
	Latency float64      `json:"latency_ms"`
	Error   string       `json:"error,omitempty"`
}

// SplitEgress is one address, and the probes that saw the router leave from it.
type SplitEgress struct {
	IP     string       `json:"ip"`
	Place  *geoip.Place `json:"place,omitempty"`
	Probes []string     `json:"probes"`
}

// SplitReport keeps every probe and groups the answers.
//
// Both halves matter: the groups answer "how many ways out is this network
// using", and the probe list keeps a failed or single-answer run honest about
// its coverage.
type SplitReport struct {
	Probes   []SplitProbeResult `json:"probes"`
	Egresses []SplitEgress      `json:"egresses"`
}

var splitClient = &http.Client{
	Timeout:   8 * time.Second,
	Transport: &http.Transport{Proxy: nil, ForceAttemptHTTP2: true},
}

func (h *HTTP) splitCheck(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), splitBudget)
	defer cancel()
	report, err := measureSplit(ctx, h.Locate)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		core.WriteError(w, http.StatusServiceUnavailable, "split check failed: "+err.Error())
		return
	}
	core.WriteJSON(w, http.StatusOK, report)
}

// measureSplit asks every vantage point at once, then locates and groups what
// came back. Grouping is by address, in the order the probes were asked, so two
// runs over the same answers produce the same page.
func measureSplit(ctx context.Context, locate func(context.Context, []netip.Addr) (map[string]geoip.Place, error)) (SplitReport, error) {
	report := SplitReport{Probes: make([]SplitProbeResult, len(splitProbes))}
	var wg sync.WaitGroup
	for i, probe := range splitProbes {
		wg.Add(1)
		go func(i int, probe splitProbe) {
			defer wg.Done()
			report.Probes[i] = runSplitProbe(ctx, probe)
		}(i, probe)
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return SplitReport{}, err
	}

	index := map[string]int{}
	for _, probe := range report.Probes {
		if probe.IP == "" {
			continue
		}
		if at, ok := index[probe.IP]; ok {
			report.Egresses[at].Probes = append(report.Egresses[at].Probes, probe.Probe)
			continue
		}
		index[probe.IP] = len(report.Egresses)
		report.Egresses = append(report.Egresses, SplitEgress{IP: probe.IP, Probes: []string{probe.Probe}})
	}

	if locate == nil || len(report.Egresses) == 0 {
		return report, nil
	}
	addrs := make([]netip.Addr, 0, len(report.Egresses))
	for _, egress := range report.Egresses {
		if addr, err := netip.ParseAddr(egress.IP); err == nil {
			addrs = append(addrs, addr)
		}
	}
	places, err := locate(ctx, addrs)
	if err != nil {
		return report, nil
	}
	for i := range report.Egresses {
		if place, ok := places[report.Egresses[i].IP]; ok {
			p := place
			report.Egresses[i].Place = &p
		}
	}
	for i := range report.Probes {
		if place, ok := places[report.Probes[i].IP]; ok {
			p := place
			report.Probes[i].Place = &p
		}
	}
	return report, nil
}

func runSplitProbe(ctx context.Context, probe splitProbe) SplitProbeResult {
	result := SplitProbeResult{Probe: probe.name, Region: probe.region}
	start := time.Now()
	token, err := fetchSplitProbe(ctx, probe)
	result.Latency = ms(time.Since(start))
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.IP = token
	return result
}

func fetchSplitProbe(ctx context.Context, probe splitProbe) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probe.url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "open-linux-router")
	resp, err := splitClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	address, err := probe.read(resp)
	if err != nil {
		return "", err
	}
	addr, err := netip.ParseAddr(address)
	if err != nil || !addr.IsGlobalUnicast() {
		return "", fmt.Errorf("no public address in the answer")
	}
	return addr.Unmap().String(), nil
}

func readBareAddress(resp *http.Response) (string, error) {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(body)), nil
}

// readTraceIP reads the `ip=` line of a trace page, which is the address that
// edge saw rather than a derived one.
func readTraceIP(resp *http.Response) (string, error) {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if err != nil {
		return "", err
	}
	for line := range strings.Lines(string(body)) {
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), "ip="); ok {
			return value, nil
		}
	}
	return "", errors.New("the trace did not name an address")
}

// readAddressInText finds the address inside a sentence, for providers that
// answer "当前 IP：203.0.113.7 来自于：…" rather than with the address alone.
func readAddressInText(resp *http.Response) (string, error) {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if err != nil {
		return "", err
	}
	for _, field := range strings.FieldsFunc(string(body), func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == ':' || r == '：' || r == '='
	}) {
		if addr, err := netip.ParseAddr(strings.TrimSpace(field)); err == nil {
			return addr.String(), nil
		}
	}
	return "", errors.New("the answer carried no address")
}
