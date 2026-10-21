package tools

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
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

func TestQualityOverallNormalizesMeasuredWeights(t *testing.T) {
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
	actual, total := aggregateQuality([]QualityCategory{partial, category("wifi", "Wi-Fi", 15, QualityComponent{Weight: 1})}, QualitySnapshot{}, time.Now())
	if total.Score == nil || *total.Score != 80 || total.Earned != 80 || total.Available != 100 || total.Coverage != 10 || total.State != "ready" {
		t.Fatalf("unmeasured components must not reduce the score: %+v", total)
	}
	if actual[0].Available != 100 || actual[0].Earned != 80 || actual[0].Components[0].EffectiveWeight != 1 || actual[0].Components[1].EffectiveWeight != 0 || actual[1].Available != 0 {
		t.Fatalf("effective weights: %+v", actual)
	}
	// A measured failure stays in both denominators; it is never omitted.
	zero := category("latency", "Latency", 25, QualityComponent{Weight: .6, Score: valuePointer(0)}, QualityComponent{Weight: .4, Score: valuePointer(100)})
	actual, total = aggregateQuality([]QualityCategory{partial, zero}, QualitySnapshot{}, time.Now())
	if total.Score == nil || *total.Score != 60 || actual[0].Available != 50 || actual[1].Available != 50 {
		t.Fatalf("weighted measured categories: %+v", total)
	}
	_, total = aggregateQuality([]QualityCategory{category("failure", "Failure", 25, QualityComponent{Weight: .4, Score: valuePointer(0)})}, QualitySnapshot{}, time.Now())
	if total.Score == nil || *total.Score != 0 || total.Available != 100 || total.State != "ready" {
		t.Fatalf("measured zero must remain a real total: %+v", total)
	}
	for _, state := range []string{"running", "cancelled", "timed_out"} {
		_, total = aggregateQuality([]QualityCategory{partial}, QualitySnapshot{State: state}, time.Now())
		if total.Score != nil {
			t.Fatalf("%s published a finished total: %+v", state, total)
		}
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
	if total.Score == nil || total.Available != 100 || total.State != "ready" {
		t.Fatalf("missing capabilities must not prevent a normalized total: %+v", total)
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

func TestQualityAlternativesCompareNormalizedScores(t *testing.T) {
	partial := qualityFixture("ok", 10)
	for i := range partial {
		partial[i].DNS, partial[i].Response = nil, nil
	}
	results := []QualityTargetResult{
		{ID: "test", Family: "ipv4", Samples: qualityFixture("ok", 60)},
		{ID: "test", Family: "ipv6", Samples: partial},
	}
	scores := timingScores(bestFamily(results, "test", false))
	if scores[0] == nil || *scores[0] != 100 || scores[1] != nil || scores[2] != nil {
		t.Fatal("alternatives must compare measured scores and keep the selected family's evidence together")
	}
}

func TestQualityCurvesAndRetention(t *testing.T) {
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
	if categories[0].State == "stale" || categories[0].Score == nil || total.State != "ready" || total.Score == nil || total.Available != 100 {
		t.Fatalf("retained evidence lost its score: %+v", total)
	}
	_, later := scoreQuality(QualitySnapshot{Speed: &SpeedResult{Download: 75, Upload: 30}, ExpiresAt: &expires}, expires.Add(365*24*time.Hour))
	if later.Score == nil || *later.Score != *total.Score {
		t.Fatalf("score changed with age: %+v", later)
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
		QualityPath: filepath.Join(t.TempDir(), "quality.json"),
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
	if probes.Load() != 36 || transfers.Load() != 1 || result.Speed == nil || result.NAT == nil || result.Overall.Score == nil || math.Abs(*result.Overall.Score-8000.0/85) > 1e-9 || result.Overall.Available != 100 {
		t.Fatalf("result=%+v probes=%d transfers=%d", result, probes.Load(), transfers.Load())
	}
	// A new light run must not reuse a previous bandwidth result.
	h2 := &HTTP{QualityPath: h.QualityPath}
	if err := h2.RestoreQuality(); err != nil {
		t.Fatal(err)
	}
	restored := h2.qualitySnapshot()
	if !reflect.DeepEqual(result, restored) {
		t.Fatalf("assessment changed after restart: before=%+v after=%+v", result, restored)
	}
	info, err := os.Stat(h.QualityPath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("checkpoint permissions: info=%v err=%v", info, err)
	}
	w = httptest.NewRecorder()
	routes.ServeHTTP(w, httptest.NewRequest("POST", "/quality", nil))
	result = waitQuality(t, h)
	if result.Speed != nil || transfers.Load() != 1 {
		t.Fatal("light run reused or started a bandwidth test")
	}
	if result.Overall.Score == nil || math.Abs(*result.Overall.Score-100) > 1e-9 || result.Categories[0].Available != 0 || result.Categories[3].Available != 0 {
		t.Fatalf("light assessment must use the full 100 points without speed or Wi-Fi: %+v", result.Overall)
	}
}

func TestRestoreQualityMissingAndCorruptCheckpoint(t *testing.T) {
	h := &HTTP{QualityPath: filepath.Join(t.TempDir(), "quality.json")}
	if err := h.RestoreQuality(); err != nil || h.qualitySnapshot().State != "idle" {
		t.Fatalf("missing checkpoint: %v", err)
	}
	for _, data := range []string{"{", `{"state":"running"}`, `null`} {
		if err := os.WriteFile(h.QualityPath, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := h.RestoreQuality(); err == nil || h.qualitySnapshot().State != "idle" {
			t.Fatalf("invalid checkpoint %q: %v", data, err)
		}
	}
}

func TestQualityCheckpointSurvivesInterruptedRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quality.json")
	h := &HTTP{QualityPath: path}
	h.quality.snapshot = QualitySnapshot{State: "completed", RunID: "previous", Speed: &SpeedResult{Download: 75, Upload: 30}}
	h.saveQuality()
	started := make(chan struct{}, 6)
	h.qualityProbe = func(ctx context.Context, _ qualityTarget, _ string) QualitySample {
		started <- struct{}{}
		<-ctx.Done()
		return QualitySample{State: "cancelled"}
	}
	defer h.Close()
	w := httptest.NewRecorder()
	h.startQuality(w, httptest.NewRequest("POST", "/quality", nil))
	if w.Code != http.StatusAccepted {
		t.Fatalf("start: %d", w.Code)
	}
	<-started
	restored := &HTTP{QualityPath: path}
	if err := restored.RestoreQuality(); err != nil {
		t.Fatal(err)
	}
	if snapshot := restored.qualitySnapshot(); snapshot.RunID != "previous" || snapshot.Overall.Score == nil {
		t.Fatalf("running assessment replaced saved result: %+v", snapshot)
	}
	h.Close()
	result := waitQuality(t, h)
	if err := restored.RestoreQuality(); err != nil {
		t.Fatal(err)
	}
	if snapshot := restored.qualitySnapshot(); snapshot.State != "cancelled" || !reflect.DeepEqual(result, snapshot) {
		t.Fatalf("shutdown evidence not restored: %+v", snapshot)
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

func waitQualitySettled(t *testing.T, h *HTTP) QualitySnapshot {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		snapshot := h.qualitySnapshot()
		if snapshot.State != "running" && snapshot.RunningScope == "" {
			return snapshot
		}
		select {
		case <-deadline.C:
			t.Fatal("scoped assessment did not finish")
			return QualitySnapshot{}
		case <-tick.C:
		}
	}
}

func qualityTargetResult(snapshot QualitySnapshot, id, family string) QualityTargetResult {
	for _, result := range snapshot.Targets {
		if result.ID == id && result.Family == family {
			return result
		}
	}
	return QualityTargetResult{}
}

func qualityComponent(category QualityCategory, id string) QualityComponent {
	for _, component := range category.Components {
		if component.ID == id {
			return component
		}
	}
	return QualityComponent{}
}

func qualityCategory(snapshot QualitySnapshot, id string) QualityCategory {
	for _, category := range snapshot.Categories {
		if category.ID == id {
			return category
		}
	}
	return QualityCategory{}
}

func TestQualityScopedRerunsMergeEvidenceAndUpdateScores(t *testing.T) {
	var probes, transfers atomic.Int32
	var slow atomic.Bool
	h := &HTTP{
		QualityPath: filepath.Join(t.TempDir(), "quality.json"),
		qualityProbe: func(_ context.Context, target qualityTarget, _ string) QualitySample {
			probes.Add(1)
			tcp := 10.0
			switch target.ID {
			case "google":
				tcp = 60
			case "github":
				tcp = 30
			}
			if slow.Load() && target.ID == "cloudflare" {
				tcp = 300
			}
			return qualityFixture("ok", tcp)[0]
		},
		Run: func(context.Context) (SpeedResult, error) {
			n := float64(transfers.Add(1))
			return SpeedResult{Server: "Reference", Download: 75 * n, Upload: 30 * n}, nil
		},
		RunNAT: func(context.Context) (NATResult, error) { return NATResult{Mapped: "203.0.113.1:1234"}, nil },
	}
	defer h.Close()
	routes := core.RouteTable(h.Routes())

	request := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		routes.ServeHTTP(w, httptest.NewRequest("POST", path, nil))
		return w
	}

	if w := request("/quality?speed=true"); w.Code != http.StatusAccepted {
		t.Fatalf("full run: %d %s", w.Code, w.Body.String())
	}
	before := waitQualitySettled(t, h)
	if probes.Load() != 36 || transfers.Load() != 1 || before.Speed == nil || before.Overall.Score == nil {
		t.Fatalf("full run: probes=%d transfers=%d snapshot=%+v", probes.Load(), transfers.Load(), before)
	}
	if component := qualityComponent(qualityCategory(before, "latency"), "cloudflare-tcp"); component.Target != "cloudflare" || component.Kind != "tcp" {
		t.Fatalf("component metadata: %+v", component)
	}
	if component := qualityComponent(qualityCategory(before, "speed"), "reference"); component.Kind != "reference" {
		t.Fatalf("reference metadata: %+v", component)
	}

	slow.Store(true)
	if w := request("/quality?scope=target&target=cloudflare"); w.Code != http.StatusAccepted {
		t.Fatalf("target rerun: %d %s", w.Code, w.Body.String())
	}
	target := waitQualitySettled(t, h)
	if probes.Load() != 48 || transfers.Load() != 1 {
		t.Fatalf("target rerun touched the wrong tests: probes=%d transfers=%d", probes.Load(), transfers.Load())
	}
	if got := qualityTargetResult(target, "cloudflare", "ipv4").Samples[0].TCP; got == nil || *got != 300 {
		t.Fatalf("cloudflare was not refreshed: %v", got)
	}
	if got := qualityTargetResult(target, "google", "ipv4").Samples[0].TCP; got == nil || *got != 60 {
		t.Fatalf("google changed outside its scope: %v", got)
	}
	if target.Speed == nil || target.Speed.Download != 75 {
		t.Fatalf("target rerun replaced the speed evidence: %+v", target.Speed)
	}
	if target.Overall.Score == nil || *target.Overall.Score == *before.Overall.Score {
		t.Fatalf("target rerun did not update the total: before=%v after=%v", before.Overall.Score, target.Overall.Score)
	}
	if target.CompletedAt == nil || before.CompletedAt == nil || !target.CompletedAt.After(*before.CompletedAt) {
		t.Fatalf("target rerun did not update its completion time: before=%v after=%v", before.CompletedAt, target.CompletedAt)
	}

	if w := request("/quality?scope=speed"); w.Code != http.StatusAccepted {
		t.Fatalf("speed rerun: %d %s", w.Code, w.Body.String())
	}
	speed := waitQualitySettled(t, h)
	if probes.Load() != 48 || transfers.Load() != 2 || speed.Speed == nil || speed.Speed.Download != 150 || speed.SpeedError != "" {
		t.Fatalf("speed rerun: probes=%d transfers=%d speed=%+v error=%q", probes.Load(), transfers.Load(), speed.Speed, speed.SpeedError)
	}
	if got := qualityTargetResult(speed, "cloudflare", "ipv4").Samples[0].TCP; got == nil || *got != 300 {
		t.Fatalf("speed rerun replaced target evidence: %v", got)
	}

	if w := request("/quality?scope=responses"); w.Code != http.StatusAccepted {
		t.Fatalf("responses rerun: %d %s", w.Code, w.Body.String())
	}
	responses := waitQualitySettled(t, h)
	if probes.Load() != 84 || transfers.Load() != 2 || responses.Speed == nil || responses.Speed.Download != 150 {
		t.Fatalf("responses rerun: probes=%d transfers=%d speed=%+v", probes.Load(), transfers.Load(), responses.Speed)
	}

	for _, path := range []string{
		"/quality?scope=bogus",
		"/quality?scope=target",
		"/quality?scope=target&target=example",
		"/quality?scope=target&target=cloudflare&speed=true",
	} {
		if w := request(path); w.Code != http.StatusBadRequest {
			t.Errorf("%s status=%d body=%s", path, w.Code, w.Body.String())
		}
	}

	// A scope has nothing to refresh until a full assessment exists.
	idle := &HTTP{qualityProbe: func(context.Context, qualityTarget, string) QualitySample {
		t.Error("a scoped rerun probed without an assessment")
		return QualitySample{}
	}}
	w := httptest.NewRecorder()
	core.RouteTable(idle.Routes()).ServeHTTP(w, httptest.NewRequest("POST", "/quality?scope=responses", nil))
	if w.Code != http.StatusConflict {
		t.Fatalf("scoped run on idle status=%d", w.Code)
	}
}

func TestQualityScopedRunShowsProgressAndCancellationKeepsEvidence(t *testing.T) {
	var targets []QualityTargetResult
	for _, target := range qualityTargets {
		for _, family := range []string{"ipv4", "ipv6"} {
			targets = append(targets, QualityTargetResult{ID: target.ID, Name: target.Name, URL: target.URL, Family: family, Samples: qualityFixture("ok", 10)})
		}
	}
	completed := time.Now().UTC().Add(-time.Hour)
	path := filepath.Join(t.TempDir(), "quality.json")
	h := &HTTP{QualityPath: path}
	h.quality.snapshot = QualitySnapshot{
		State: "completed", RunID: "previous", Targets: targets,
		Speed: &SpeedResult{Download: 75, Upload: 30}, CompletedAt: &completed,
	}
	h.saveQuality()
	started := make(chan struct{}, 2*qualitySamples)
	h.qualityProbe = func(ctx context.Context, _ qualityTarget, _ string) QualitySample {
		started <- struct{}{}
		<-ctx.Done()
		return QualitySample{State: "cancelled"}
	}
	defer h.Close()
	routes := core.RouteTable(h.Routes())
	w := httptest.NewRecorder()
	routes.ServeHTTP(w, httptest.NewRequest("POST", "/quality?scope=target&target=cloudflare", nil))
	if w.Code != http.StatusAccepted {
		t.Fatalf("scoped run: %d %s", w.Code, w.Body.String())
	}
	var progress QualitySnapshot
	if err := json.Unmarshal(w.Body.Bytes(), &progress); err != nil {
		t.Fatal(err)
	}
	if progress.State != "completed" || progress.RunningScope != "target" || progress.RunningTarget != "cloudflare" || progress.Overall.Score == nil {
		t.Fatalf("scoped progress replaced the published result: %+v", progress)
	}
	for range 2 {
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatal("scoped probes did not start")
		}
	}
	w = httptest.NewRecorder()
	routes.ServeHTTP(w, httptest.NewRequest("POST", "/quality", nil))
	if w.Code != http.StatusConflict {
		t.Fatalf("concurrent full run status=%d", w.Code)
	}
	w = httptest.NewRecorder()
	routes.ServeHTTP(w, httptest.NewRequest("DELETE", "/quality", nil))
	result := waitQualitySettled(t, h)
	if result.State != "completed" || result.RunningScope != "" || result.RunningTarget != "" || h.running.Load() || h.natRunning.Load() {
		t.Fatalf("cancelled scope state: %+v running=%v nat=%v", result, h.running.Load(), h.natRunning.Load())
	}
	if !reflect.DeepEqual(result.Targets, targets) || result.Speed == nil || result.Speed.Download != 75 {
		t.Fatalf("cancelled scope changed evidence: %+v", result.Targets)
	}
	if result.CompletedAt == nil || !result.CompletedAt.Equal(completed) {
		t.Fatalf("cancelled scope changed its completion time: %v", result.CompletedAt)
	}
	restored := &HTTP{QualityPath: path}
	if err := restored.RestoreQuality(); err != nil {
		t.Fatal(err)
	}
	checkpoint := restored.qualitySnapshot()
	if checkpoint.RunningScope != "" || !reflect.DeepEqual(checkpoint.Targets, targets) || checkpoint.Speed == nil || checkpoint.Speed.Download != 75 {
		t.Fatalf("cancelled scope changed the checkpoint: %+v", checkpoint)
	}
}
