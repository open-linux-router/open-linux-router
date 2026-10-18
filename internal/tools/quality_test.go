package tools

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

func qualityFixture(state string, tcp float64) []QualitySample {
	samples := make([]QualitySample, qualitySamples)
	for i := range samples {
		samples[i] = QualitySample{State: state, Stage: "complete"}
		if state == "ok" || state == "inconclusive" {
			samples[i].TCP, samples[i].DNS, samples[i].Response = valuePointer(tcp), valuePointer(5), valuePointer(20)
		}
	}
	return samples
}

func TestQualityOverallBudgetAndPartialCoverage(t *testing.T) {
	budgets := []float64{25, 25, 25, 15, 10}
	scores := []float64{80, 90, 70, 80, 60}
	var categories []QualityCategory
	for i, budget := range budgets {
		categories = append(categories, category("", "", budget, QualityComponent{Weight: 1, Score: &scores[i]}))
	}
	_, total := aggregateQuality(categories, QualitySnapshot{}, time.Now())
	if total.Score == nil || *total.Score != 78 || total.Available != 100 || total.State != "ready" {
		t.Fatalf("complete total: %+v", total)
	}
	partial := category("speed", "Speed", 25,
		QualityComponent{Weight: .4, Score: valuePointer(80)},
		QualityComponent{Weight: .6})
	_, total = aggregateQuality([]QualityCategory{partial}, QualitySnapshot{}, time.Now())
	if total.Score != nil || total.Earned != 8 || total.Available != 10 || total.Coverage != 10 || partial.Score == nil || *partial.Score != 80 {
		t.Fatalf("partial must not become 80/100 overall: category=%+v total=%+v", partial, total)
	}
}

func TestQualityAlternativesAndServiceBreadth(t *testing.T) {
	var results []QualityTargetResult
	for i, target := range qualityTargets {
		state := "failed"
		if i == 0 {
			state = "ok"
		}
		results = append(results,
			QualityTargetResult{ID: target.ID, Family: "ipv4", Samples: qualityFixture("failed", 0)},
			QualityTargetResult{ID: target.ID, Family: "ipv6", Samples: qualityFixture(state, 10)})
	}
	if score := accessScore(results, qualityTargets[0].ID); score == nil || *score != 100 {
		t.Fatalf("working IPv6 must not be averaged with failed IPv4: %v", score)
	}
	if score := accessScore(results, qualityTargets[1].ID); score == nil || *score != 0 {
		t.Fatalf("confirmed failures must remain scored zero: %v", score)
	}
	categories, total := scoreQuality(QualitySnapshot{Targets: results}, time.Now())
	functionality := categories[4]
	if functionality.Score == nil || math.Abs(*functionality.Score-100.0/3) > 1e-9 {
		t.Fatalf("one working service must not hide two failing services: %+v", functionality)
	}
	if total.Score != nil || total.Available >= 100 {
		t.Fatalf("missing capabilities must not create full total: %+v", total)
	}
	unknown := []QualityTargetResult{{ID: "test", Samples: qualityFixture("inconclusive", 10)}}
	if accessScore(unknown, "test") != nil {
		t.Fatal("unexpected HTTP status must not be a network failure")
	}
	if accessScore(nil, "test") != nil {
		t.Fatal("missing evidence is not zero")
	}
}

func TestQualityInsufficientSamplesAndNoFakeLatency(t *testing.T) {
	failed := qualityFixture("failed", 0)
	for _, score := range timingScores(failed) {
		if score != nil {
			t.Fatal("failed connections have no measured latency")
		}
	}
	stability := stabilityScores(failed)
	if stability[0] == nil || *stability[0] != 0 || stability[1] != nil {
		t.Fatalf("failed connections: %+v", stability)
	}
	for _, score := range timingScores(qualityFixture("ok", 10)[:3]) {
		if score != nil {
			t.Fatal("too few timings must remain unscored")
		}
	}
	for _, score := range stabilityScores(qualityFixture("ok", 10)[:3]) {
		if score != nil {
			t.Fatal("partial stability run must remain unscored")
		}
	}
}

func TestQualityMeasuredZeroWinsOverMissingAlternative(t *testing.T) {
	slow := qualityFixture("ok", 1000)
	for i := range slow {
		slow[i].DNS = valuePointer(1000)
		slow[i].Response = valuePointer(3000)
	}
	results := []QualityTargetResult{
		{ID: "test", Family: "ipv4", Samples: qualityFixture("error", 0)},
		{ID: "test", Family: "ipv6", Samples: slow},
	}
	for _, score := range timingScores(bestFamily(results, "test", false)) {
		if score == nil || *score != 0 {
			t.Fatal("measured zero was replaced by missing evidence")
		}
	}
	if accessScore(results[:1], "test") != nil {
		t.Fatal("an unmeasured alternative became a failure")
	}
}

func TestQualityCurvesAndExpiry(t *testing.T) {
	for _, curve := range [][]scoreAnchor{downloadCurve, uploadCurve, tcpCurve, dnsCurve, responseCurve, variationCurve} {
		for _, invalid := range []float64{-1, math.NaN(), math.Inf(1)} {
			if scoreValue(invalid, curve) != nil {
				t.Fatal("invalid metric was scored")
			}
		}
		for _, anchor := range curve {
			if got := scoreValue(anchor.value, curve); got == nil || *got != anchor.score {
				t.Fatalf("anchor %v: %v", anchor, got)
			}
		}
	}
	expires := time.Now()
	categories, total := scoreQuality(QualitySnapshot{Speed: &SpeedResult{Download: 75, Upload: 30}, ExpiresAt: &expires}, expires)
	if categories[0].State != "stale" || total.State != "stale" || total.Score != nil || total.Available != 0 {
		t.Fatalf("stale evidence became current points: %+v", total)
	}
}

func TestQualityProbeUsesVerifiedHEADAndDoesNotFollowRedirects(t *testing.T) {
	var followed atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Errorf("method = %s", r.Method)
		}
		if r.URL.Path == "/redirect" {
			w.Header().Set("Location", "/followed")
			w.WriteHeader(http.StatusFound)
			return
		}
		if r.URL.Path == "/followed" {
			followed.Add(1)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	trust := server.Client().Transport.(*http.Transport).TLSClientConfig
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	sample := probeQualityTLS(ctx, qualityTarget{URL: server.URL, Status: 204}, "ipv4", trust)
	if sample.State != "ok" || sample.DNS == nil || sample.TCP == nil || sample.Response == nil || sample.Address == "" {
		t.Fatalf("sample: %+v", sample)
	}
	redirect := probeQualityTLS(ctx, qualityTarget{URL: server.URL + "/redirect", Status: 204}, "ipv4", trust)
	if redirect.State != "inconclusive" || redirect.Status != 302 || followed.Load() != 0 {
		t.Fatalf("redirect: %+v", redirect)
	}
	untrusted := probeQualityTLS(ctx, qualityTarget{URL: server.URL, Status: 204}, "ipv4", nil)
	if untrusted.State != "failed" || untrusted.Stage != "tls" {
		t.Fatalf("untrusted TLS was accepted: %+v", untrusted)
	}
}

func waitQuality(t *testing.T, h *HTTP) QualitySnapshot {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		if snapshot := h.qualitySnapshot(); snapshot.State != "running" {
			return snapshot
		}
		select {
		case <-deadline.C:
			t.Fatal("assessment did not finish")
			return QualitySnapshot{}
		case <-tick.C:
		}
	}
}

func TestQualityRoutesArePassiveAndPublishEvidence(t *testing.T) {
	var probes, transfers atomic.Int32
	h := &HTTP{
		qualityProbe: func(_ context.Context, target qualityTarget, family string) QualitySample {
			probes.Add(1)
			return qualityFixture("ok", 10)[0]
		},
		Run: func(context.Context) (SpeedResult, error) {
			transfers.Add(1)
			return SpeedResult{Server: "Reference", Download: 75, Upload: 30}, nil
		},
		RunNAT: func(context.Context) (NATResult, error) { return NATResult{Mapped: "203.0.113.1:1234"}, nil },
	}
	defer h.Close()
	routes := core.RouteTable(h.Routes())
	w := httptest.NewRecorder()
	routes.ServeHTTP(w, httptest.NewRequest("GET", "/quality", nil))
	if w.Code != 200 || probes.Load() != 0 || transfers.Load() != 0 {
		t.Fatal("GET launched measurements")
	}
	var empty QualitySnapshot
	if err := json.Unmarshal(w.Body.Bytes(), &empty); err != nil || empty.State != "idle" || empty.Overall.Score != nil {
		t.Fatalf("initial snapshot: %s", w.Body.String())
	}
	w = httptest.NewRecorder()
	routes.ServeHTTP(w, httptest.NewRequest("POST", "/quality?speed=true", nil))
	if w.Code != 202 {
		t.Fatalf("start: %d %s", w.Code, w.Body.String())
	}
	result := waitQuality(t, h)
	if probes.Load() != 36 || transfers.Load() != 1 || result.Speed == nil || result.NAT == nil || result.Overall.Score != nil || math.Abs(result.Overall.Available-57.5) > 1e-9 {
		t.Fatalf("result=%+v probes=%d transfers=%d", result, probes.Load(), transfers.Load())
	}
	// A new light run must not reuse a previous bandwidth result.
	w = httptest.NewRecorder()
	routes.ServeHTTP(w, httptest.NewRequest("POST", "/quality", nil))
	result = waitQuality(t, h)
	if result.Speed != nil || transfers.Load() != 1 {
		t.Fatal("light run reused or started a bandwidth test")
	}
}

func TestQualityCancellationAndSharedExclusion(t *testing.T) {
	started := make(chan struct{}, 6)
	h := &HTTP{qualityProbe: func(ctx context.Context, _ qualityTarget, _ string) QualitySample {
		started <- struct{}{}
		<-ctx.Done()
		return QualitySample{State: "cancelled"}
	}, RunNAT: func(context.Context) (NATResult, error) {
		t.Error("NAT started after cancellation")
		return NATResult{}, errors.New("cancelled")
	}}
	defer h.Close()
	routes := core.RouteTable(h.Routes())
	w := httptest.NewRecorder()
	routes.ServeHTTP(w, httptest.NewRequest("POST", "/quality", nil))
	<-started
	for _, path := range []string{"/quality", "/speedtest", "/nat"} {
		w = httptest.NewRecorder()
		routes.ServeHTTP(w, httptest.NewRequest("POST", path, nil))
		if w.Code != 409 {
			t.Errorf("%s concurrent status=%d", path, w.Code)
		}
	}
	w = httptest.NewRecorder()
	routes.ServeHTTP(w, httptest.NewRequest("DELETE", "/quality", nil))
	result := waitQuality(t, h)
	if result.State != "cancelled" || h.running.Load() || h.natRunning.Load() {
		t.Fatalf("cancellation: %+v", result)
	}
	h.Close()
	w = httptest.NewRecorder()
	routes.ServeHTTP(w, httptest.NewRequest("POST", "/quality", nil))
	if w.Code != 503 {
		t.Fatal("assessment started during shutdown")
	}
}
