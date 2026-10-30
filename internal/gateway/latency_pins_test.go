package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

func pinTestHTTP(t *testing.T) (HTTP, *core.Store) {
	t.Helper()
	store := newTestStore(t)
	custom, err := OpenCustomLatencyConfig(store)
	if err != nil {
		t.Fatal(err)
	}
	lock := core.NewLock()
	monitor, err := OpenLatencyMonitor(store, lock)
	if err != nil {
		t.Fatal(err)
	}
	monitor.Custom = custom
	monitor.probe = func(context.Context, string) (float64, error) {
		t.Error("pinning must not probe the Internet target")
		return 0, nil
	}
	custom.probe = func(context.Context, string) (float64, error) {
		t.Error("pinning must not probe monitored sites")
		return 0, nil
	}
	return HTTP{
		Latency: monitor, Lock: lock, Events: core.NewEvents(),
		Applier: Applier{Store: store, Links: testLinks(), Kernel: &StaticKernel{}},
	}, store
}

func readPinConfig(t *testing.T, store *core.Store) Config {
	t.Helper()
	doc, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := FromDocument(doc)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestLatencyPinsConfigSchema(t *testing.T) {
	schema, err := core.Reflect(ModuleName, Config{})
	if err != nil {
		t.Fatal(err)
	}
	pins, ok := schema.Full.Properties.Get("latency_pins")
	if !ok || pins.Type != "array" || pins.Items == nil || pins.Items.Type != "string" {
		t.Fatalf("latency_pins schema: %+v", pins)
	}
	if slices.Contains(schema.Full.Required, "latency_pins") {
		t.Fatal("fresh configurations must not require latency_pins")
	}
}

func TestLatencyPinsDefaultPersistenceAndIdempotency(t *testing.T) {
	h, store := pinTestHTTP(t)
	handler := h.Handler()
	events, cancel := h.Events.Subscribe()
	defer cancel()
	w := do(t, handler, http.MethodGet, "/latency", nil)
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), `"pinned"`) {
		t.Fatalf("fresh snapshot must omit false pins: %d %s", w.Code, w.Body)
	}
	for _, site := range decode[LatencySnapshot](t, w).Custom {
		if site.Pinned {
			t.Fatalf("fresh site is pinned: %+v", site)
		}
	}
	for _, pinned := range []bool{true, true, false, false, true} {
		w = do(t, handler, http.MethodPut, "/latency/sites/Google/pin", map[string]bool{"pinned": pinned})
		if w.Code != http.StatusOK {
			t.Fatalf("pin %v: %d %s", pinned, w.Code, w.Body)
		}
		snapshot := decode[LatencySnapshot](t, w)
		site := latencySite(snapshot.Custom, "Google")
		if site == nil || site.Pinned != pinned || len(snapshot.Sites) != 4 || snapshot.LocalTime.IsZero() {
			t.Fatalf("not a full, updated snapshot: %+v", snapshot)
		}
	}
	if len(events) != 3 {
		t.Fatalf("got %d events for three changes", len(events))
	}
	for range 3 {
		event := <-events
		if event.Module != ModuleName || event.Type != core.EventApplied {
			t.Fatalf("unexpected change event: %+v", event)
		}
	}
	if got := readPinConfig(t, store).LatencyPins; !reflect.DeepEqual(got, []string{"Google"}) {
		t.Fatalf("stored pins: %v", got)
	}
	reopened, err := OpenCustomLatencyConfig(core.NewStore(store.Path(), ModuleName))
	if err != nil {
		t.Fatal(err)
	}
	if site := latencySite(reopened.Snapshot(), "Google"); site == nil || !site.Pinned {
		t.Fatalf("pin lost after reopen: %+v", site)
	}
	w = do(t, handler, http.MethodPut, "/latency/sites/Google/pin", map[string]bool{"pinned": false})
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), `"pinned"`) {
		t.Fatalf("unpin: %d %s", w.Code, w.Body)
	}
	doc, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := doc.Raw(ModuleName)
	if strings.Contains(string(raw), `"latency_pins"`) {
		t.Fatalf("empty pin list should be omitted: %s", raw)
	}
}

func TestLatencyPinRequestValidation(t *testing.T) {
	h, store := pinTestHTTP(t)
	handler := h.Handler()
	for _, body := range []string{
		``, `{`, `{}`, `null`, `[]`, `{"pinned":null}`, `{"pinned":"true"}`,
		`{"pinned":1}`, `{"pinned":true,"extra":1}`, `{"pinned":true} {"pinned":false}`,
	} {
		t.Run(body, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPut, "/latency/sites/Google/pin", strings.NewReader(body))
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, request)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("invalid body status: %d %s", w.Code, w.Body)
			}
		})
	}
	for _, name := range []string{"Missing", "google"} {
		for _, pinned := range []bool{true, false} {
			w := do(t, handler, http.MethodPut, "/latency/sites/"+name+"/pin", map[string]bool{"pinned": pinned})
			if w.Code != http.StatusNotFound {
				t.Fatalf("unknown site %q status: %d %s", name, w.Code, w.Body)
			}
		}
	}
	if len(readPinConfig(t, store).LatencyPins) != 0 {
		t.Fatal("invalid requests changed stored pins")
	}
	for _, unavailable := range []HTTP{{}, {Latency: NewLatencyMonitor()}, {Latency: h.Latency}} {
		w := do(t, unavailable.Handler(), http.MethodPut, "/latency/sites/Google/pin", map[string]bool{"pinned": true})
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("missing dependency status: %d %s", w.Code, w.Body)
		}
	}
	// Authorization is rechecked by the core lock before any configuration write.
	request := httptest.NewRequest(http.MethodPut, "/latency/sites/Google/pin", strings.NewReader(`{"pinned":true}`))
	request = request.WithContext(core.WithMutationGuard(request.Context(), func() error { return errors.New("denied") }))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, request)
	if w.Code != http.StatusServiceUnavailable || len(readPinConfig(t, store).LatencyPins) != 0 {
		t.Fatalf("mutation guard bypassed: %d %s", w.Code, w.Body)
	}
}

func TestLatencyPinsFollowConfigAndCustomDeletion(t *testing.T) {
	h, store := pinTestHTTP(t)
	handler := h.Handler()
	custom := []CustomLatencySite{{Name: "NAS", URL: "https://nas.example/", Icon: "thesvg:wechat"}}
	if w := do(t, handler, http.MethodPut, "/latency/sites", custom); w.Code != http.StatusOK {
		t.Fatalf("add custom site: %d %s", w.Code, w.Body)
	}
	for _, name := range []string{"NAS", "Google"} {
		if w := do(t, handler, http.MethodPut, "/latency/sites/"+name+"/pin", map[string]bool{"pinned": true}); w.Code != http.StatusOK {
			t.Fatalf("pin %s: %d %s", name, w.Code, w.Body)
		}
	}
	// Deletion through the site's own route clears its stored pin.
	if w := do(t, handler, http.MethodPut, "/latency/sites", []CustomLatencySite{}); w.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", w.Code, w.Body)
	}
	if got := readPinConfig(t, store).LatencyPins; !reflect.DeepEqual(got, []string{"Google"}) {
		t.Fatalf("deleted pin persisted: %v", got)
	}
	if w := do(t, handler, http.MethodPut, "/latency/sites", custom); w.Code != http.StatusOK {
		t.Fatalf("re-add: %d %s", w.Code, w.Body)
	}
	if site := latencySite(h.Latency.Custom.Snapshot(), "NAS"); site == nil || site.Pinned {
		t.Fatalf("re-added site inherited a pin: %+v", site)
	}
	// General config edits are visible immediately, without reopening or probes.
	w := do(t, handler, http.MethodPatch, "/config", map[string]any{"latency_pins": []string{"NAS"}})
	if w.Code != http.StatusOK {
		t.Fatalf("config pin edit: %d %s", w.Code, w.Body)
	}
	snapshot := decode[LatencySnapshot](t, do(t, handler, http.MethodGet, "/latency", nil))
	if !latencySite(snapshot.Custom, "NAS").Pinned || latencySite(snapshot.Custom, "Google").Pinned {
		t.Fatalf("general config edit did not refresh pins: %+v", snapshot.Custom)
	}
	// General config deletion also prunes the pin, even if the patch leaves it alone.
	w = do(t, handler, http.MethodPatch, "/config", map[string]any{"latency_sites": []CustomLatencySite{}})
	if w.Code != http.StatusOK || len(readPinConfig(t, store).LatencyPins) != 0 {
		t.Fatalf("config deletion kept pins: %d %s", w.Code, w.Body)
	}
}

func TestLatencyPinsConcurrentUpdatesPreserveProbesAndConfig(t *testing.T) {
	h, store := pinTestHTTP(t)
	handler := h.Handler()
	cfg := testConfig()
	cfg.LatencySites = []CustomLatencySite{{Name: "NAS", URL: "https://nas.example/", Icon: "thesvg:wechat"}}
	cfg.LatencyCommon = []CommonLatency{{Name: "Google", Exit: "Proxy"}}
	cfg.LatencyTarget = latencyTargets()[1].URL
	if w := do(t, handler, http.MethodPut, "/config", cfg); w.Code != http.StatusOK {
		t.Fatalf("seed config: %d %s", w.Code, w.Body)
	}
	before := readPinConfig(t, store)
	h.Latency.Custom.Snapshot()
	value := 11.0
	now := time.Now().UTC().Add(-time.Minute)
	h.Latency.Custom.mu.Lock()
	h.Latency.Custom.storeResult(LatencySite{URL: cfg.LatencySites[0].URL, CheckedAt: now, Milliseconds: &value})
	h.Latency.Custom.mu.Unlock()
	started := make(chan struct{})
	release := make(chan struct{})
	h.Latency.Custom.probe = func(context.Context, string) (float64, error) {
		close(started)
		<-release
		return 22, nil
	}
	var releaseOnce sync.Once
	releaseProbe := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseProbe()
	if err := h.Latency.Custom.Measure("NAS"); err != nil {
		t.Fatal(err)
	}
	<-started
	probeBefore := *latencySite(h.Latency.Custom.Snapshot(), "NAS")
	if !probeBefore.Measuring || len(probeBefore.Samples) != 1 {
		t.Fatalf("probe setup: %+v", probeBefore)
	}
	// Independent requests share one lock, so no pin can overwrite another pin
	// or a target/config change loaded by a different writer.
	var wg sync.WaitGroup
	for _, name := range []string{"NAS", "Google", "Baidu", "GitHub"} {
		wg.Go(func() {
			w := do(t, handler, http.MethodPut, "/latency/sites/"+name+"/pin", map[string]bool{"pinned": true})
			if w.Code != http.StatusOK {
				t.Errorf("pin %s: %d %s", name, w.Code, w.Body)
			}
		})
	}
	wg.Go(func() {
		w := do(t, handler, http.MethodPut, "/latency/target", map[string]string{"target": latencyTargets()[3].URL})
		if w.Code != http.StatusOK {
			t.Errorf("target: %d %s", w.Code, w.Body)
		}
	})
	wg.Go(func() {
		w := do(t, handler, http.MethodPatch, "/config", map[string]any{"stats": false})
		if w.Code != http.StatusOK {
			t.Errorf("config: %d %s", w.Code, w.Body)
		}
	})
	wg.Wait()
	after := readPinConfig(t, store)
	if !reflect.DeepEqual(after.LatencyPins, []string{"Baidu", "GitHub", "Google", "NAS"}) || after.LatencyTarget != latencyTargets()[3].URL {
		t.Fatalf("concurrent update lost intent: %+v", after)
	}
	before.LatencyTarget = after.LatencyTarget
	before.LatencyPins = after.LatencyPins
	before.Stats = after.Stats
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("pins changed other intent:\nbefore: %+v\nafter: %+v", before, after)
	}
	probeAfter := *latencySite(h.Latency.Custom.Snapshot(), "NAS")
	probeAfter.Pinned = false
	if !reflect.DeepEqual(probeBefore, probeAfter) {
		t.Fatalf("pinning disturbed in-flight probe/history:\nbefore: %+v\nafter: %+v", probeBefore, probeAfter)
	}
	releaseProbe()
	deadline := time.Now().Add(3 * time.Second)
	for {
		completed := latencySite(h.Latency.Custom.Snapshot(), "NAS")
		if !completed.Measuring {
			if !completed.Pinned || completed.Milliseconds == nil || *completed.Milliseconds != 22 || len(completed.Samples) == 0 {
				t.Fatalf("probe completion lost pin/results: %+v", completed)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("probe did not finish: %+v", completed)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestLatencyPinPersistenceFailureDoesNotPublish(t *testing.T) {
	h, store := pinTestHTTP(t)
	events, cancel := h.Events.Subscribe()
	defer cancel()
	doc, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	// A configuration unreadable by the gateway must not be replaced by a
	// partial document containing only the requested pin.
	doc.Set(ModuleName, json.RawMessage(`{"unknown_field":true}`))
	if err := store.Save(doc); err != nil {
		t.Fatal(err)
	}
	w := do(t, h.Handler(), http.MethodPut, "/latency/sites/Google/pin", map[string]bool{"pinned": true})
	if w.Code != http.StatusServiceUnavailable || len(events) != 0 {
		t.Fatalf("failed persistence: %d %s events=%d", w.Code, w.Body, len(events))
	}
	if site := latencySite(h.Latency.Custom.Snapshot(), "Google"); site.Pinned {
		t.Fatal("failed write changed live pins")
	}
}
