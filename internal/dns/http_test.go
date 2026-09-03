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

// The UI polls these endpoints, and the relay holds thousands of entries. The
// default has to stay unbounded, though: `olr dns queries` reads the same route,
// and a limit applied without being asked for would make the CLI truncate a log
// an operator asked to see in full.
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

func (s stubObserver) Queries(context.Context) ([]Query, Stats, error) {
	return s.queries, s.stats, nil
}
func (s stubObserver) Names(context.Context) ([]Name, Stats, error) { return s.names, s.stats, nil }
func (s stubObserver) Clients(context.Context) ([]Client, error)    { return s.stats.Clients, nil }

func TestUnknownRouteIs404(t *testing.T) {
	h, _, _ := testHTTP(t)
	if w := do(t, h, http.MethodGet, "/nope", ""); w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}
