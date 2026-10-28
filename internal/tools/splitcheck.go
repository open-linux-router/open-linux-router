package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/geoip"
)

// The split check asks Ookla's public speed-test fleet which address it sees
// this router leave from. The fleet is the same one a browser speed test uses:
// each server answers `GET /getip` with the caller's address over HTTP on port
// 8080, following the redirect to its canonical name. Thousands of servers in
// hundreds of networks mean the answers describe where this network actually
// sends traffic, rather than one operator's opinion of it.

const (
	// splitBudget bounds the whole run, including assembling the server list.
	splitBudget = 180 * time.Second
	// splitConcurrency bounds the probes in flight. Each is one small request;
	// this finishes a full run without asking a router to hold a thousand
	// connections for a diagnostic.
	splitConcurrency = 32
	// splitFleetCap bounds how many servers one run asks. The fleet behind the
	// list is larger; asking all of it would trade time for little more
	// evidence.
	splitFleetCap = 1200
	// splitFleetTTL keeps runs from re-reading the server list every time.
	splitFleetTTL = 6 * time.Hour
)

// splitCountries are the searches that assemble the fleet. Ookla's server list
// answers with servers near the caller unless asked for a place, so the fleet is
// built by asking for each of these and merging the answers. The list is fixed
// and covers every continent rather than every country: it exists to sample
// destinations, not to enumerate them.
var splitCountries = []string{
	"United States", "Canada", "Mexico", "Brazil", "Argentina", "Chile", "Colombia",
	"United Kingdom", "Ireland", "Germany", "France", "Netherlands", "Belgium",
	"Spain", "Italy", "Poland", "Sweden", "Norway", "Finland", "Switzerland",
	"Austria", "Portugal", "Czechia", "Romania", "Greece", "Russia", "Turkey",
	"United Arab Emirates", "Saudi Arabia", "Israel", "Egypt", "Nigeria", "Kenya",
	"South Africa", "India", "China", "Japan", "South Korea", "Singapore",
	"Hong Kong", "Taiwan", "Thailand", "Vietnam", "Indonesia", "Malaysia",
	"Philippines", "Australia", "New Zealand",
}

// splitServer is one entry of the fleet, narrowed to what the report shows.
type splitServer struct {
	host    string
	country string
	sponsor string
}

// SplitEgress is one address, and the servers that saw the router leave from
// it. Servers is a sample: with a thousand probes in one group the count and
// the country list are the answer, and these rows are the evidence.
type SplitEgress struct {
	IP        string       `json:"ip"`
	Place     *geoip.Place `json:"place,omitempty"`
	Count     int          `json:"count"`
	Countries []string     `json:"countries"`
	Servers   []string     `json:"servers"`
}

type SplitFailure struct {
	Server string `json:"server"`
	Error  string `json:"error"`
}

// SplitReport keeps the totals, so a partly answered run cannot read as a
// complete one.
type SplitReport struct {
	Asked    int            `json:"asked"`
	Answered int            `json:"answered"`
	Egresses []SplitEgress  `json:"egresses"`
	Failures []SplitFailure `json:"failures"`
}

// splitClient ignores environment proxies for the same reason every other probe
// does: the answer must describe this router's path, not a proxy's.
var splitClient = &http.Client{
	Timeout:   8 * time.Second,
	Transport: &http.Transport{Proxy: nil, ForceAttemptHTTP2: true},
}

// The fleet and the single-server probe are variables so a test can stand in
// for the network without standing in for the logic under test.
var (
	splitServers   = loadSplitFleet
	splitProbeOnce = probeSplitServer
)

func (h *HTTP) splitCheck(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), splitBudget)
	defer cancel()
	report, err := measureSplit(ctx, splitServers, splitProbeOnce, h.Locate)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		core.WriteError(w, http.StatusServiceUnavailable, "split check failed: "+err.Error())
		return
	}
	core.WriteJSON(w, http.StatusOK, report)
}

// measureSplit asks every server, then groups the answers by address.
func measureSplit(
	ctx context.Context,
	fleet func(context.Context) ([]splitServer, error),
	probe func(context.Context, splitServer) (string, error),
	locate func(context.Context, []netip.Addr) (map[string]geoip.Place, error),
) (SplitReport, error) {
	servers, err := fleet(ctx)
	if err != nil {
		return SplitReport{}, err
	}
	report := SplitReport{Asked: len(servers)}
	type answer struct {
		server splitServer
		ip     string
		err    error
	}
	answers := make([]answer, len(servers))
	work := make(chan int)
	var wg sync.WaitGroup
	for range splitConcurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range work {
				ip, err := probe(ctx, servers[i])
				answers[i] = answer{server: servers[i], ip: ip, err: err}
			}
		}()
	}
	for i := range servers {
		select {
		case work <- i:
		case <-ctx.Done():
			close(work)
			wg.Wait()
			return SplitReport{}, ctx.Err()
		}
	}
	close(work)
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return SplitReport{}, err
	}

	groups := map[string]*SplitEgress{}
	var order []string
	for _, a := range answers {
		if a.err != nil {
			if len(report.Failures) < 20 {
				report.Failures = append(report.Failures, SplitFailure{Server: serverLabel(a.server), Error: a.err.Error()})
			}
			continue
		}
		report.Answered++
		group, ok := groups[a.ip]
		if !ok {
			group = &SplitEgress{IP: a.ip}
			groups[a.ip] = group
			order = append(order, a.ip)
		}
		group.Count++
		group.Countries = appendUnique(group.Countries, a.server.country)
		if len(group.Servers) < 4 {
			group.Servers = append(group.Servers, serverLabel(a.server))
		}
	}
	for _, ip := range order {
		e := groups[ip]
		sort.Strings(e.Countries)
		report.Egresses = append(report.Egresses, *e)
	}
	sort.SliceStable(report.Egresses, func(i, j int) bool {
		if report.Egresses[i].Count != report.Egresses[j].Count {
			return report.Egresses[i].Count > report.Egresses[j].Count
		}
		return report.Egresses[i].IP < report.Egresses[j].IP
	})

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
	return report, nil
}

func appendUnique(list []string, value string) []string {
	if value == "" {
		return list
	}
	for _, existing := range list {
		if existing == value {
			return list
		}
	}
	return append(list, value)
}

func serverLabel(server splitServer) string {
	if server.country == "" {
		return server.sponsor
	}
	return server.sponsor + " · " + server.country
}

// loadSplitFleet assembles the fleet from Ookla's server list, one search per
// country, merged round-robin so a capped run still spans the list rather than
// stopping inside the first countries.
func loadSplitFleet(ctx context.Context) ([]splitServer, error) {
	splitFleetMu.Lock()
	defer splitFleetMu.Unlock()
	if time.Since(splitFleetAt) < splitFleetTTL && len(splitFleet) > 0 {
		return splitFleet, nil
	}

	answers := make([][]splitServer, len(splitCountries))
	var errs []string
	var wg sync.WaitGroup
	work := make(chan int)
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range work {
				servers, err := searchSplitCountry(ctx, splitCountries[i])
				if err != nil {
					errs = append(errs, splitCountries[i]+": "+err.Error())
					continue
				}
				answers[i] = servers
			}
		}()
	}
	for i := range splitCountries {
		work <- i
	}
	close(work)
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var fleet []splitServer
	seen := map[string]bool{}
	for round := 0; len(fleet) < splitFleetCap; round++ {
		added := false
		for _, answer := range answers {
			if round >= len(answer) {
				continue
			}
			server := answer[round]
			if seen[server.host] {
				continue
			}
			seen[server.host] = true
			fleet = append(fleet, server)
			added = true
			if len(fleet) == splitFleetCap {
				break
			}
		}
		if !added {
			break
		}
	}
	if len(fleet) == 0 {
		if len(errs) > 0 {
			return nil, errors.New("the server list could not be read: " + strings.Join(errs, "; "))
		}
		return nil, errors.New("the server list was empty")
	}
	splitFleet, splitFleetAt = fleet, time.Now()
	return fleet, nil
}

var (
	splitFleetMu sync.Mutex
	splitFleet   []splitServer
	splitFleetAt time.Time
)

// splitServersURL is Ookla's nearby-server payload. The search parameter is
// what turns "near me" into "somewhere I asked for".
var splitServersURL = "https://www.speedtest.net/api/js/servers"

func searchSplitCountry(ctx context.Context, country string) ([]splitServer, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, splitServersURL, nil)
	if err != nil {
		return nil, err
	}
	query := request.URL.Query()
	query.Set("engine", "js")
	query.Set("limit", "100")
	query.Set("search", country)
	request.URL.RawQuery = query.Encode()
	// The list endpoint answers 403 without a browser-shaped request.
	request.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 Chrome/140.0 Safari/537.36")
	request.Header.Set("Accept", "application/json, text/plain, */*")
	request.Header.Set("Referer", "https://www.speedtest.net/")
	request.Header.Set("Origin", "https://www.speedtest.net")
	resp, err := splitClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var entries []struct {
		Host    string `json:"host"`
		Country string `json:"country"`
		Sponsor string `json:"sponsor"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&entries); err != nil {
		return nil, err
	}
	servers := make([]splitServer, 0, len(entries))
	for _, entry := range entries {
		if entry.Host == "" {
			continue
		}
		servers = append(servers, splitServer{host: entry.Host, country: entry.Country, sponsor: entry.Sponsor})
	}
	return servers, nil
}

// probeSplitServer asks one server which address it sees us leave from.
//
// The canonical host answers `GET /getip` over HTTP with the address as text; a
// server that still answers on its own name redirects there first, which the
// client follows. A server that cannot answer is a failed probe, not a reason
// to stop the run.
func probeSplitServer(ctx context.Context, server splitServer) (string, error) {
	url := "http://" + hostWithPort(server.host) + "/getip?nocache=" + fmt.Sprint(time.Now().UnixNano())
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("User-Agent", "open-linux-router")
	resp, err := splitClient.Do(request)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256))
	if err != nil {
		return "", err
	}
	addr, err := netip.ParseAddr(strings.TrimSpace(string(body)))
	if err != nil || !publicAddress(addr) {
		return "", errors.New("the answer carried no public address")
	}
	return addr.Unmap().String(), nil
}

// publicAddress rejects answers that are not routable on the internet. A
// private, loopback, link-local or carrier-grade NAT address means the server
// saw something other than this router's public egress, and reporting it as an
// egress would be worse than reporting a failed probe.
func publicAddress(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsValid() || !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() {
		return false
	}
	if addr.Is4() {
		b := addr.As4()
		if b[0] == 100 && b[1] >= 64 && b[1] <= 127 {
			return false
		}
	}
	return true
}

// hostWithPort keeps the fleet's own port when it has one. The list already
// carries :8080, but a host without a port is still reachable on it.
func hostWithPort(host string) string {
	if strings.Contains(host, ":") {
		return host
	}
	return host + ":8080"
}
