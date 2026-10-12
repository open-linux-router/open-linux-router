package dns

import (
	"bytes"
	"os"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

func TestStoppingDNSRequiresConfirmationBeforeAnyWrite(t *testing.T) {
	a, resolver, relay := testApplier(t)
	resolver.active, relay.active = true, true
	a.Observer = fakeObserver{}
	h := HTTP{Applier: a, Lock: core.NewLock(), Events: core.NewEvents()}.Handler()
	if w := do(t, h, "PUT", "/config", validPUT); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	before, _ := os.ReadFile(a.Store.Path())
	if w := do(t, h, "PATCH", "/config", `{"enabled":false}`); w.Code != 409 {
		t.Fatal(w.Code, w.Body)
	}
	if after, _ := os.ReadFile(a.Store.Path()); !bytes.Equal(before, after) {
		t.Fatal("refused stop changed intent")
	}
	if w := do(t, h, "PATCH", "/config?confirm=true", `{"enabled":false}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
}
