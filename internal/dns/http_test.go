package dns

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// testHTTP wires the module's HTTP surface onto the same temp-rooted Applier
// the apply tests use, so nothing here needs root, systemd or /etc.
func testHTTP(t *testing.T) (http.Handler, *fakeService, *fakeService) {
	t.Helper()
	applier, resolver, relay := testApplier(t)
	resolver.active, relay.active = true, true
	applier.Observer = fakeObserver{}

	h := HTTP{Applier: applier, Lock: core.NewLock(), Events: core.NewEvents()}
	return h.Handler(), resolver, relay
}

func do(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

const validPUT = `{
  "enabled": true,
  "listen": ["192.168.1.1:53"],
  "allow_from": ["192.168.1.0/24"],
  "query_log": {"enabled": true}
}`

func TestConfigRoundTripsThroughHTTP(t *testing.T) {
	h, _, _ := testHTTP(t)

	if w := do(t, h, http.MethodPut, "/config", validPUT); w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body %s", w.Code, w.Body)
	}

	w := do(t, h, http.MethodGet, "/config", "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET status = %d", w.Code)
	}
	var got Config
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Enabled || len(got.Listen) != 1 || got.Listen[0].String() != "192.168.1.1:53" {
		t.Errorf("config did not round trip: %+v", got)
	}
}

// A mistyped key that silently did nothing is the worst outcome here: a 200, an
// operator who believes the setting took, and a blocklist that never loaded.
func TestPutRejectsUnknownFields(t *testing.T) {
	h, _, _ := testHTTP(t)
	w := do(t, h, http.MethodPut, "/config", `{"enabled":true,"blocklist":["x"]}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body %s", w.Code, w.Body)
	}
}

func TestPutRejectsAnInvalidConfig(t *testing.T) {
	h, _, _ := testHTTP(t)
	// An open resolver, which validation refuses outright.
	w := do(t, h, http.MethodPut, "/config",
		`{"enabled":true,"listen":["192.168.1.1:53"],"allow_from":["0.0.0.0/0"]}`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body %s", w.Code, w.Body)
	}
	// The complaint has to be addressed to the field that caused it, or a UI
	// cannot attach it to anything.
	var body map[string]core.ErrorBody
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body["error"].Problems) == 0 || body["error"].Problems[0].Path == "" {
		t.Errorf("the error carries no addressed problem: %s", w.Body)
	}
}

// Arrays are replaced wholesale by RFC 7386, which is why adding one blocked
// name is a PUT rather than a PATCH — worth pinning so nobody relies on a merge.
func TestPatchChangesOneFieldAndReplacesArrays(t *testing.T) {
	h, _, _ := testHTTP(t)
	if w := do(t, h, http.MethodPut, "/config", validPUT); w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body %s", w.Code, w.Body)
	}

	if w := do(t, h, http.MethodPatch, "/config", `{"query_log":{"enabled":false}}`); w.Code != http.StatusOK {
		t.Fatalf("PATCH status = %d, body %s", w.Code, w.Body)
	}

	w := do(t, h, http.MethodGet, "/config", "")
	var got Config
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.QueryLog.Enabled {
		t.Error("the patched field did not change")
	}
	if len(got.Listen) != 1 {
		t.Errorf("an untouched field was lost: %+v", got)
	}
}

// The first switch on a fresh box: turn DNS on, say nothing else.
//
// This was a 422 about a field the operator had never opened. Then it was a 200
// that chose an address and stored it — which is what went stale the first time
// a network changed underneath a box, because a stored address is a copy of
// something the kernel owns.
//
// It is now a 200 that stores no address at all. The relay binds the wildcard,
// and what olr decided for the operator is reported rather than written down:
// design.md §5.6 asks for the sentence, not for the copy.
func TestEnablingDnsStoresNoAddress(t *testing.T) {
	h, _, _ := testHTTP(t)

	w := do(t, h, http.MethodPatch, "/config", `{"enabled":true}`)
	if w.Code != http.StatusOK {
		t.Fatalf("PATCH status = %d, want 200; body %s", w.Code, w.Body)
	}

	var applied applyResponse
	if err := json.Unmarshal(w.Body.Bytes(), &applied); err != nil {
		t.Fatal(err)
	}
	if len(applied.Plan.Derived) == 0 {
		t.Fatal("turned DNS on without saying what it decided")
	}
	joined := strings.Join(applied.Plan.Derived, "\n")
	// Who may resolve is the decision worth publishing: an empty allow_from
	// looks like "no restriction" and means the opposite.
	if !strings.Contains(joined, "192.168.1.0/24") {
		t.Errorf("derived %q, want the network it will resolve for", applied.Plan.Derived)
	}

	var stored Config
	if err := json.Unmarshal(do(t, h, http.MethodGet, "/config", "").Body.Bytes(), &stored); err != nil {
		t.Fatal(err)
	}
	if len(stored.Listen) != 0 {
		t.Errorf("stored listen = %v, want nothing: an address here is the thing that goes stale",
			stored.Listen)
	}
}

// The preview has to agree with the apply, or the confirm dialog is describing
// a different change from the one the button makes.
func TestPlanFillsInTheSameAddressAsApply(t *testing.T) {
	h, _, _ := testHTTP(t)

	w := do(t, h, http.MethodPost, "/plan", `{"enabled":true}`)
	if w.Code != http.StatusOK {
		t.Fatalf("POST /plan status = %d, body %s", w.Code, w.Body)
	}
	var plan planView
	if err := json.Unmarshal(w.Body.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if len(plan.Derived) == 0 {
		t.Fatal("the preview did not report the address it would choose")
	}
	if plan.Empty {
		t.Error("turning DNS on previewed as no change at all")
	}
}

func TestPatchRejectsAnEmptyBody(t *testing.T) {
	h, _, _ := testHTTP(t)
	if w := do(t, h, http.MethodPatch, "/config", ""); w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// An empty body plans the stored intent, which by §5.4 is exactly the drift
// check.
func TestPlanWithNoBodyIsTheDriftCheck(t *testing.T) {
	h, _, _ := testHTTP(t)
	if w := do(t, h, http.MethodPut, "/config", validPUT); w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body %s", w.Code, w.Body)
	}

	w := do(t, h, http.MethodPost, "/plan", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", w.Code, w.Body)
	}
	var plan planView
	if err := json.Unmarshal(w.Body.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if !plan.Empty {
		t.Errorf("planning a just-applied config reported work: %+v", plan)
	}
}

// The case no other route could reach: every rendered file is right and a
// backend that should be running is not.
//
// There is no edit to the configuration that repairs that. The plan is one
// service action and an empty change list, so a surface with only PUT /config
// can show an operator the pending start and offer them no way to run it —
// which is what the web UI did, on a box whose relay was enabled after its last
// boot and therefore never started by anything.
func TestApplyWithNoBodyStartsABackendThatIsNotRunning(t *testing.T) {
	h, _, relay := testHTTP(t)
	if w := do(t, h, http.MethodPut, "/config", validPUT); w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body %s", w.Code, w.Body)
	}

	// The relay is not running, and nothing else about the box has changed.
	relay.active = false
	relay.calls = nil

	w := do(t, h, http.MethodPost, "/apply", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", w.Code, w.Body)
	}
	var resp applyResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}

	// The assertion that makes this a repair rather than a write: stored intent
	// produced no file change, because stored intent was never the problem.
	if len(resp.Plan.Changes) != 0 {
		t.Errorf("re-applying stored intent rewrote files: %+v", resp.Plan.Changes)
	}
	if !relay.active {
		t.Error("the relay is still not running after an apply")
	}
	var started bool
	for _, c := range relay.calls {
		if c == "start" {
			started = true
		}
	}
	if !started {
		t.Errorf("the relay was never started; calls = %v", relay.calls)
	}
}

// A body is not merged into stored intent, because then "put it back" would be
// a write nobody asked for. The route takes what is stored and nothing else.
func TestApplyIgnoresAnyBody(t *testing.T) {
	h, _, _ := testHTTP(t)
	if w := do(t, h, http.MethodPut, "/config", validPUT); w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body %s", w.Code, w.Body)
	}

	if w := do(t, h, http.MethodPost, "/apply", `{"enabled": false}`); w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", w.Code, w.Body)
	}

	w := do(t, h, http.MethodGet, "/config", "")
	var got Config
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Enabled {
		t.Error("a body sent to /apply switched DNS off; it must apply stored intent only")
	}
}

// The impact classification is only actionable next to the lines that caused
// it: "disruptive" with no diff is a scarier spinner, not an explanation.
func TestPlanCarriesTheDiffAndTheImpact(t *testing.T) {
	h, _, _ := testHTTP(t)
	if w := do(t, h, http.MethodPut, "/config", validPUT); w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body %s", w.Code, w.Body)
	}

	w := do(t, h, http.MethodPost, "/plan", `{
	  "enabled": true,
	  "listen": ["192.168.1.1:53"],
	  "allow_from": ["192.168.1.0/24"],
	  "query_log": {"enabled": true},
	  "policies": [{"name":"kids","block":["example.com"]}]
	}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", w.Code, w.Body)
	}

	var plan planView
	if err := json.Unmarshal(w.Body.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.Empty {
		t.Fatal("adding a policy planned no work")
	}
	if plan.Impact != ImpactReload {
		t.Errorf("impact = %s, want reload", plan.Impact)
	}
	if len(plan.Changes) == 0 || plan.Changes[0].Diff == "" {
		t.Errorf("the plan carries no diff: %+v", plan.Changes)
	}
	// The warning about a blocklist nothing enforces is the whole point of a
	// preview here.
	if len(plan.Warnings) == 0 {
		t.Error("the plan surfaced no warnings")
	}
}

// A change that has not been applied must not touch anything.
func TestPlanDoesNotApply(t *testing.T) {
	h, _, relay := testHTTP(t)
	if w := do(t, h, http.MethodPut, "/config", validPUT); w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body %s", w.Code, w.Body)
	}
	relay.calls = nil

	do(t, h, http.MethodPost, "/plan", `{
	  "enabled": true,
	  "listen": ["192.168.1.1:53"],
	  "allow_from": ["192.168.1.0/24"],
	  "policies": [{"name":"kids","block":["example.com"]}]
	}`)

	if len(relay.calls) != 0 {
		t.Errorf("a dry run signalled the relay: %v", relay.calls)
	}
	w := do(t, h, http.MethodGet, "/config", "")
	var got Config
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Policies) != 0 {
		t.Errorf("a dry run stored intent: %+v", got.Policies)
	}
}

// Each part of "health" is reported independently and none can suppress the
// others — with two backends there are three questions, not one.
func TestStatusReportsBothBackendsSeparately(t *testing.T) {
	h, resolver, _ := testHTTP(t)
	if w := do(t, h, http.MethodPut, "/config", validPUT); w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body %s", w.Code, w.Body)
	}
	resolver.statusErr = errors.New("no system bus")

	w := do(t, h, http.MethodGet, "/status", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", w.Code, w.Body)
	}
	var resp statusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Services) != 2 {
		t.Fatalf("want both backends reported, got %d", len(resp.Services))
	}
	if resp.Services[0].Error == "" {
		t.Error("the resolver's failure was not reported")
	}
	if resp.Services[1].Status == nil {
		t.Error("the relay's state was suppressed by the resolver's failure")
	}
	if resp.AsOf.IsZero() {
		t.Error("the reply is not stamped with its freshness")
	}
}

// The relay being down is a state of the system, not a fault in olrd, and the
// difference tells the operator which thing to go and look at.
func TestQueriesReportsAStoppedRelayAsUnavailable(t *testing.T) {
	applier, resolver, relay := testApplier(t)
	resolver.active, relay.active = true, true
	applier.Observer = fakeObserver{err: errors.New("connection refused")}
	h := HTTP{Applier: applier, Lock: core.NewLock(), Events: core.NewEvents()}.Handler()

	if w := do(t, h, http.MethodGet, "/queries", ""); w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503; body %s", w.Code, w.Body)
	}
}

func TestQueriesAndNamesAreStamped(t *testing.T) {
	applier, resolver, relay := testApplier(t)
	resolver.active, relay.active = true, true
	applier.Observer = stubObserver{
		queries: []Query{{
			At: time.Now(), Client: netip.MustParseAddr("192.168.1.10"),
			Name: "example.com", Type: "A", Rcode: "NOERROR",
			Answers: []netip.Addr{netip.MustParseAddr("93.184.216.34")},
		}},
		names: []Name{{
			Client: netip.MustParseAddr("192.168.1.10"), Name: "example.com",
			Addr: netip.MustParseAddr("93.184.216.34"), Expires: time.Now().Add(time.Hour),
		}},
		stats: Stats{Since: time.Now(), Queries: 1, Dropped: 3, Capacity: 100, Held: 1},
	}
	h := HTTP{Applier: applier, Lock: core.NewLock(), Events: core.NewEvents()}.Handler()

	w := do(t, h, http.MethodGet, "/queries", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", w.Code, w.Body)
	}
	var queries queriesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &queries); err != nil {
		t.Fatal(err)
	}
	if len(queries.Queries) != 1 || queries.Queries[0].Client != "192.168.1.10" {
		t.Errorf("query log did not survive the boundary: %+v", queries.Queries)
	}
	if queries.AsOf.IsZero() {
		t.Error("the reply is not stamped with its freshness")
	}
	// The gap has to be visible, or a log that shed entries under load would
	// look complete.
	if queries.Stats == nil || queries.Stats.Dropped != 3 {
		t.Errorf("the dropped-observation count was not published: %+v", queries.Stats)
	}

	w = do(t, h, http.MethodGet, "/names", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", w.Code, w.Body)
	}
	var names namesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &names); err != nil {
		t.Fatal(err)
	}
	if len(names.Names) != 1 || names.Names[0].Address != "93.184.216.34" {
		t.Errorf("name map did not survive the boundary: %+v", names.Names)
	}
}

// Status reports the counters and must not read the log to get them.
//
// It used to: it asked for the queries, kept the stats that came back with them
// and dropped the rest. Every poll of the overview page — one every five
// seconds — therefore moved the whole ring across the observation socket to
// read six integers. The counting stub is the only way to state that as a test,
// because the symptom was never in the response body.
func TestStatusReadsTheCountersWithoutTheLog(t *testing.T) {
	applier, resolver, relay := testApplier(t)
	resolver.active, relay.active = true, true

	counting := &countingObserver{stats: Stats{Since: time.Now(), Queries: 12, Held: 5, Capacity: 100}}
	applier.Observer = counting
	h := HTTP{Applier: applier, Lock: core.NewLock(), Events: core.NewEvents()}.Handler()

	w := do(t, h, http.MethodGet, "/status", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", w.Code, w.Body)
	}

	if counting.queries != 0 {
		t.Errorf("status read the query log %d times, want 0", counting.queries)
	}
	if counting.names != 0 {
		t.Errorf("status read the name map %d times, want 0", counting.names)
	}
	if counting.statsCalls != 1 {
		t.Errorf("status read the counters %d times, want 1", counting.statsCalls)
	}
	// One, and from the drift half: Plan asks who is resolving through us so it
	// can tell a harmless access-control change from one that cuts somebody off.
	// It is the same cheap /stats read, so it is counted separately rather than
	// mistaken for a second pass at the counters.
	if counting.clients != 1 {
		t.Errorf("status asked who is connected %d times, want 1", counting.clients)
	}

	var resp statusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Stats == nil || resp.Stats.Queries != 12 {
		t.Errorf("the counters did not survive the change: %+v", resp.Stats)
	}
}

// countingObserver records which reads a handler actually made.
type countingObserver struct {
	stats      Stats
	queries    int
	names      int
	statsCalls int
	clients    int
}

func (c *countingObserver) Queries(context.Context, int) ([]Query, Stats, error) {
	c.queries++
	return nil, c.stats, nil
}

func (c *countingObserver) Names(context.Context, int) ([]Name, Stats, error) {
	c.names++
	return nil, c.stats, nil
}

func (c *countingObserver) Stats(context.Context) (Stats, error) {
	c.statsCalls++
	return c.stats, nil
}

func (c *countingObserver) Clients(context.Context) ([]Client, error) {
	c.clients++
	return c.stats.Clients, nil
}

// The UI polls these endpoints, and the relay holds thousands of entries. The
// default has to stay unbounded, though: `olr dns queries` reads the same route,
// and a limit applied without being asked for would make the CLI truncate a log
// an operator asked to see in full.
//
// The bound reaches the relay rather than trimming what it sent: the stub
// honours the limit, so a handler that read the parameter and forgot to pass it
// on fails here.
func TestQueriesAndNamesTakeAnOptionalLimit(t *testing.T) {
	applier, resolver, relay := testApplier(t)
	resolver.active, relay.active = true, true

	var queries []Query
	var names []Name
	for i := 0; i < 5; i++ {
		queries = append(queries, Query{
			At: time.Now(), Client: netip.MustParseAddr("192.168.1.10"),
			Name: "example.com", Type: "A", Rcode: "NOERROR",
		})
		names = append(names, Name{
			Client: netip.MustParseAddr("192.168.1.10"), Name: "example.com",
			Addr: netip.MustParseAddr("93.184.216.34"), Expires: time.Now().Add(time.Hour),
		})
	}
	applier.Observer = stubObserver{
		queries: queries, names: names,
		stats: Stats{Since: time.Now(), Queries: 5, Held: 5, Capacity: 100},
	}
	h := HTTP{Applier: applier, Lock: core.NewLock(), Events: core.NewEvents()}.Handler()

	read := func(t *testing.T, path string) queriesResponse {
		t.Helper()
		w := do(t, h, http.MethodGet, path, "")
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, body %s", path, w.Code, w.Body)
		}
		var resp queriesResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		return resp
	}

	if got := read(t, "/queries"); len(got.Queries) != 5 {
		t.Errorf("no limit returned %d queries, want all 5", len(got.Queries))
	}

	got := read(t, "/queries?limit=2")
	if len(got.Queries) != 2 {
		t.Errorf("limit=2 returned %d queries, want 2", len(got.Queries))
	}
	// The count the caller is not being shown still has to be reachable, or a
	// truncated list looks like the whole of a quiet network.
	if got.Stats == nil || got.Stats.Held != 5 {
		t.Errorf("held count did not survive the limit: %+v", got.Stats)
	}

	// Asking for more than there is is not an error, and must not pad.
	if got := read(t, "/queries?limit=50"); len(got.Queries) != 5 {
		t.Errorf("limit=50 returned %d queries, want 5", len(got.Queries))
	}

	w := do(t, h, http.MethodGet, "/names?limit=3", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", w.Code, w.Body)
	}
	var namesResp namesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &namesResp); err != nil {
		t.Fatal(err)
	}
	if len(namesResp.Names) != 3 {
		t.Errorf("limit=3 returned %d names, want 3", len(namesResp.Names))
	}

	// Refused rather than ignored: a client that meant to bound the response
	// and mistyped it should hear so, not receive everything.
	for _, bad := range []string{"/queries?limit=nope", "/queries?limit=-1", "/names?limit=x"} {
		if w := do(t, h, http.MethodGet, bad, ""); w.Code != http.StatusBadRequest {
			t.Errorf("GET %s status = %d, want 400", bad, w.Code)
		}
	}
}

// stubObserver returns fixed observations.
type stubObserver struct {
	queries []Query
	names   []Name
	stats   Stats
}

// The limit is honoured here, not ignored, because it is now the relay's job
// rather than the handler's — a stub that returned everything regardless would
// let a handler that dropped the parameter on the floor pass.
func (s stubObserver) Queries(_ context.Context, limit int) ([]Query, Stats, error) {
	return stubLimit(s.queries, limit), s.stats, nil
}
func (s stubObserver) Names(_ context.Context, limit int) ([]Name, Stats, error) {
	return stubLimit(s.names, limit), s.stats, nil
}
func (s stubObserver) Stats(context.Context) (Stats, error) {
	return s.stats, nil
}
func (s stubObserver) Clients(context.Context) ([]Client, error) {
	return s.stats.Clients, nil
}

func stubLimit[T any](in []T, limit int) []T {
	if limit < 0 || limit >= len(in) {
		return in
	}
	return in[:limit]
}

func TestUnknownRouteIs404(t *testing.T) {
	h, _, _ := testHTTP(t)
	if w := do(t, h, http.MethodGet, "/nope", ""); w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// An id nobody offered is refused, and refused as a 404 rather than a 500 — it
// is a request for something that does not exist here, not a fault in olrd.
//
// The ids are resolved against blockers re-derived at the moment of the call
// rather than against the ones the client read earlier, so this also covers the
// case that matters more: a conflict somebody else has already cleared cannot be
// "fixed" a second time.
//
// Deliberately the only fix test at this level. A POST with an empty body on a
// box that genuinely is missing a backend would run that box's package manager,
// and a test suite must not install anything on the machine running it — which
// is why the behaviour either side of this line is tested in internal/core
// against a fake one.
func TestFixingAnUnknownBlockerIs404(t *testing.T) {
	h, _, _ := testHTTP(t)

	w := do(t, h, http.MethodPost, "/blockers/fix", `{"ids":["install:no-such-thing"]}`)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404; body %s", w.Code, w.Body)
	}
	if !strings.Contains(w.Body.String(), "no-such-thing") {
		t.Errorf("the refusal does not name what was asked for: %s", w.Body)
	}
}

// A body that is not JSON is a client error, not a reason to start installing
// things because the ids came back empty.
func TestFixRejectsAMalformedBody(t *testing.T) {
	h, _, _ := testHTTP(t)

	if w := do(t, h, http.MethodPost, "/blockers/fix", `{"ids":`); w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body %s", w.Code, w.Body)
	}
}
