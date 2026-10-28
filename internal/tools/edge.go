package tools

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// edgeBudget bounds the whole check. Each CDN answers one request; a provider
// that needs more than this is treated as unreachable rather than waited on.
const edgeBudget = 12 * time.Second

// EdgeResult is one CDN's answer to "which of your points of presence is
// serving this router".
//
// POP is the airport-style code the CDN itself uses, when its answer carries
// one. Token keeps the provider's original string so a reader can tell HKG from
// HKG61-P1 instead of trusting our parsing. A probe that failed still carries
// its name and latency, because which CDN failed is part of the answer.
type EdgeResult struct {
	CDN     string  `json:"cdn"`
	POP     string  `json:"pop,omitempty"`
	Token   string  `json:"token,omitempty"`
	Latency float64 `json:"latency_ms"`
	Error   string  `json:"error,omitempty"`
}

type EdgeReport struct {
	Edges []EdgeResult `json:"edges"`
}

// edgeProbe is one provider's self-report endpoint. The catalog is fixed: a
// CDN is here only when it volunteers its edge node in a response a browser or
// this router can already fetch, so no provider is inferred from latency.
type edgeProbe struct {
	cdn  string
	url  string
	read func(*http.Response) (string, error)
}

// edgeProbes are deliberately few. Each entry is a public endpoint that names
// the node that answered; adding a provider means proving it does, not adding a
// guess.
var edgeProbes = []edgeProbe{
	{cdn: "Cloudflare", url: "https://www.cloudflare.com/cdn-cgi/trace", read: readCloudflareTrace},
	{cdn: "CloudFront", url: "https://aws.amazon.com/", read: readHeader("X-Amz-Cf-Pop", false)},
	{cdn: "Fastly", url: "https://www.fastly.com/", read: readHeader("X-Served-By", true)},
}

// edgeClient ignores environment proxies for the same reason the speed tests
// do: a proxy in the environment would make this report the proxy's edge rather
// than the network this router is on.
var edgeClient = &http.Client{
	Timeout:   10 * time.Second,
	Transport: &http.Transport{Proxy: nil, ForceAttemptHTTP2: true},
}

func (h *HTTP) edge(w http.ResponseWriter, r *http.Request) {
	run := h.RunEdge
	if run == nil {
		run = measureEdge
	}
	ctx, cancel := context.WithTimeout(r.Context(), edgeBudget)
	defer cancel()
	report, err := run(ctx)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		core.WriteError(w, http.StatusServiceUnavailable, "edge check failed: "+err.Error())
		return
	}
	core.WriteJSON(w, http.StatusOK, report)
}

// measureEdge asks every provider at once and answers in catalog order.
//
// One provider failing is not a failed check: the report keeps its row with the
// error attached, so "Fastly timed out" is a finding rather than a blank page.
func measureEdge(ctx context.Context) (EdgeReport, error) {
	report := EdgeReport{Edges: make([]EdgeResult, len(edgeProbes))}
	var wg sync.WaitGroup
	for i, probe := range edgeProbes {
		wg.Add(1)
		go func(i int, probe edgeProbe) {
			defer wg.Done()
			start := time.Now()
			token, err := fetchEdge(ctx, probe)
			result := EdgeResult{CDN: probe.cdn, Latency: ms(time.Since(start))}
			if err != nil {
				result.Error = err.Error()
			} else {
				result.Token = token
				result.POP = edgePOP(probe.cdn, token)
			}
			report.Edges[i] = result
		}(i, probe)
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return EdgeReport{}, err
	}
	return report, nil
}

func fetchEdge(ctx context.Context, probe edgeProbe) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probe.url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "open-linux-router")
	resp, err := edgeClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		return "", fmt.Errorf("%s answered HTTP %d", probe.cdn, resp.StatusCode)
	}
	token, err := probe.read(resp)
	if err != nil {
		return "", err
	}
	if token == "" {
		return "", errors.New("the answer named no edge node")
	}
	return token, nil
}

// readCloudflareTrace reads the `colo=` line from Cloudflare's trace page. The
// page is a list of key=value lines; colo is the airport code of the edge that
// answered.
func readCloudflareTrace(resp *http.Response) (string, error) {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	if err != nil {
		return "", err
	}
	for line := range strings.Lines(string(body)) {
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), "colo="); ok {
			return value, nil
		}
	}
	return "", errors.New("the trace did not name an edge")
}

// readHeader takes a provider that names its edge in a response header.
//
// list keeps the last comma-separated value, which is the edge that answered;
// earlier entries belong to hops behind it (an origin shield, for example).
func readHeader(name string, list bool) func(*http.Response) (string, error) {
	return func(resp *http.Response) (string, error) {
		value := strings.TrimSpace(resp.Header.Get(name))
		if value == "" {
			return "", fmt.Errorf("the answer carried no %s header", name)
		}
		if list {
			if parts := strings.Split(value, ","); len(parts) > 1 {
				value = strings.TrimSpace(parts[len(parts)-1])
			}
		}
		return value, nil
	}
}

// edgePOP narrows a provider's token to the airport code it contains.
//
// Cloudflare already answers with the code. CloudFront appends a node number
// and a partition (HKG61-P1), and Fastly prefixes the cache hierarchy
// (cache-hkg-vhhh8650052-HKG). Anything that does not match keeps its token:
// showing an unfamiliar code is better than showing a wrong one.
func edgePOP(cdn, token string) string {
	switch cdn {
	case "CloudFront":
		if end := strings.IndexFunc(token, func(r rune) bool { return r < 'A' || r > 'Z' }); end > 0 {
			return token[:end]
		}
		return token
	case "Fastly":
		if i := strings.LastIndexByte(token, '-'); i >= 0 && i+1 < len(token) {
			return token[i+1:]
		}
		return token
	default:
		return token
	}
}
