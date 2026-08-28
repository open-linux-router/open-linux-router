package dhcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// testHTTP wires the module's HTTP surface onto the same temp-rooted Applier
// the apply tests use, so nothing here needs root, systemd or /etc.
func testHTTP(t *testing.T) (http.Handler, Applier, *core.Lock) {
	t.Helper()
	applier, svc := testApplier(t)
	svc.active = true

	lock := core.NewLock()
	h := HTTP{Applier: applier, Lock: lock, Events: core.NewEvents()}
	return h.Handler(), applier, lock
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
  "pools": [{"interface":"br-lan","start":"192.168.1.100","end":"192.168.1.200","lease_time":"12h"}]
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
	if !got.Enabled || len(got.Pools) != 1 || got.Pools[0].Interface != "br-lan" {
		t.Fatalf("stored config did not survive the round trip: %+v", got)
	}
	if got.Pools[0].LeaseTime != Duration(12*60*60*1e9) {
		t.Errorf("lease time = %v, want 12h", got.Pools[0].LeaseTime)
	}
}

// design.md §5.4: drift is planning unchanged intent against reality and
// finding the diff empty. Straight after an apply, that must hold.
func TestPlanningStoredIntentAfterApplyIsEmpty(t *testing.T) {
	h, _, _ := testHTTP(t)

	if w := do(t, h, http.MethodPut, "/config", validPUT); w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body %s", w.Code, w.Body)
	}

	w := do(t, h, http.MethodPost, "/plan", "")
	if w.Code != http.StatusOK {
		t.Fatalf("plan status = %d, body %s", w.Code, w.Body)
	}
	var plan planView
	if err := json.Unmarshal(w.Body.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if !plan.Empty {
		t.Errorf("drift detected right after applying: %+v", plan.Changes)
	}
}

// The whole value of validation is that it happens before anything is written
// (§5.3.1), so a rejected request must leave no trace on disk.
func TestInvalidConfigIsRejectedBeforeAnythingIsWritten(t *testing.T) {
	h, applier, _ := testHTTP(t)

	const outsideSubnet = `{"enabled":true,"pools":[{"interface":"br-lan","start":"10.9.9.5","end":"10.9.9.9"}]}`

	w := do(t, h, http.MethodPut, "/config", outsideSubnet)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body %s", w.Code, w.Body)
	}

	var body struct {
		Error core.ErrorBody `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	// Addressed by path so a UI can attach it to the field that caused it.
	if len(body.Error.Problems) == 0 || body.Error.Problems[0].Path == "" {
		t.Errorf("no field-addressed problem returned: %+v", body.Error)
	}

	if _, err := os.Stat(applier.Store.Path()); !os.IsNotExist(err) {
		t.Error("a rejected config was written to disk anyway")
	}
	if _, err := os.Stat(applier.Paths.Conf); !os.IsNotExist(err) {
		t.Error("a rejected config rendered a backend file")
	}
}

func TestUnknownFieldsAreRejected(t *testing.T) {
	h, _, _ := testHTTP(t)

	for _, method := range []string{http.MethodPut, http.MethodPatch} {
		w := do(t, h, method, "/config", `{"enabledd":true}`)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s status = %d, want 400; body %s", method, w.Code, w.Body)
		}
	}
}

// PATCH is what `olr set` and a UI toggle both need: change one field and leave
// the rest alone. §10 requires the relaxed projection for exactly this.
func TestPatchChangesOneFieldAndLeavesTheRest(t *testing.T) {
	h, _, _ := testHTTP(t)

	if w := do(t, h, http.MethodPut, "/config", validPUT); w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body %s", w.Code, w.Body)
	}
	if w := do(t, h, http.MethodPatch, "/config", `{"enabled":false}`); w.Code != http.StatusOK {
		t.Fatalf("PATCH status = %d, body %s", w.Code, w.Body)
	}

	w := do(t, h, http.MethodGet, "/config", "")
	var got Config
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Enabled {
		t.Error("PATCH did not disable the module")
	}
	if len(got.Pools) != 1 {
		t.Errorf("PATCH dropped the pools it never mentioned: %+v", got.Pools)
	}
}

func TestEmptyPatchIsRejected(t *testing.T) {
	h, _, _ := testHTTP(t)
	if w := do(t, h, http.MethodPatch, "/config", ""); w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// §3.6: every config write takes the one global lock.
func TestWritesTakeTheApplyLock(t *testing.T) {
	h, _, lock := testHTTP(t)

	// Hold the lock, then confirm a write cannot proceed while it is held.
	held := make(chan struct{})
	release := make(chan struct{})
	go func() {
		_ = lock.Do(context.Background(), func() error {
			close(held)
			<-release
			return nil
		})
	}()
	<-held

	done := make(chan int, 1)
	go func() {
		done <- do(t, h, http.MethodPut, "/config", validPUT).Code
	}()

	select {
	case code := <-done:
		t.Fatalf("a write completed (%d) while the apply lock was held", code)
	case <-timeAfter():
	}

	close(release)
	if code := <-done; code != http.StatusOK {
		t.Errorf("status after the lock was released = %d", code)
	}
}

// §3.6: reads never take the lock. A status page that froze during an apply
// would be broken in exactly the moment the operator is watching it.
func TestReadsDoNotTakeTheApplyLock(t *testing.T) {
	h, _, lock := testHTTP(t)

	held := make(chan struct{})
	release := make(chan struct{})
	go func() {
		_ = lock.Do(context.Background(), func() error {
			close(held)
			<-release
			return nil
		})
	}()
	<-held
	defer close(release)

	for _, path := range []string{"/config", "/status", "/leases"} {
		done := make(chan int, 1)
		go func() { done <- do(t, h, http.MethodGet, path, "").Code }()

		select {
		case code := <-done:
			if code != http.StatusOK {
				t.Errorf("GET %s status = %d", path, code)
			}
		case <-timeAfter():
			t.Errorf("GET %s blocked behind the apply lock", path)
		}
	}
}

// Each half of "health" is reported independently: §5.4 treats drift and
// backend liveness as two questions, and one failing must not suppress the
// other.
func TestStatusReportsDriftAndLivenessSeparately(t *testing.T) {
	h, _, _ := testHTTP(t)

	if w := do(t, h, http.MethodPut, "/config", validPUT); w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body %s", w.Code, w.Body)
	}

	w := do(t, h, http.MethodGet, "/status", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", w.Code, w.Body)
	}
	var got statusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Enabled {
		t.Error("enabled was not reported")
	}
	if got.Drifted {
		t.Error("drift reported straight after an apply")
	}
	if got.Service == nil {
		t.Error("no service status reported")
	}
	// §4.5: every observed object carries its freshness.
	if got.AsOf.IsZero() {
		t.Error("status carries no as_of")
	}
}

func TestLeasesAreStampedWithTheirFreshness(t *testing.T) {
	h, _, _ := testHTTP(t)

	w := do(t, h, http.MethodGet, "/leases", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", w.Code, w.Body)
	}
	var got leasesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.AsOf.IsZero() {
		t.Error("leases carry no as_of")
	}
}

// The plan is a dry run: design.md §5.1's --dry-run over HTTP. It must not
// change anything.
func TestPlanDoesNotApply(t *testing.T) {
	h, applier, _ := testHTTP(t)

	w := do(t, h, http.MethodPost, "/plan", validPUT)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", w.Code, w.Body)
	}
	var plan planView
	if err := json.Unmarshal(w.Body.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.Empty {
		t.Error("planning a fresh config reported nothing to do")
	}
	// The diff is what makes the impact class actionable rather than alarming.
	if len(plan.Changes) == 0 || plan.Changes[0].Diff == "" {
		t.Error("plan carried no diff")
	}

	if _, err := os.Stat(applier.Store.Path()); !os.IsNotExist(err) {
		t.Error("a plan wrote the config to disk")
	}
}

// timeAfter is the window used to assert that a request is or is not blocked.
// Long enough not to be flaky on a loaded build machine, short enough that a
// genuine deadlock does not stall the suite.
func timeAfter() <-chan time.Time { return time.After(250 * time.Millisecond) }
