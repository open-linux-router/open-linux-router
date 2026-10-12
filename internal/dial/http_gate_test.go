package dial

import (
	"bytes"
	"net/http/httptest"
	"net/netip"
	"os"
	"strings"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

func TestDefaultRouteChangeRequiresServerConfirmation(t *testing.T) {
	a := Applier{Store: testStore(t, ""), Writer: fakeWriter{observed: Observed{
		Present: true, Up: true, Addrs: []netip.Prefix{netip.MustParsePrefix("192.168.1.2/24")},
		Gateway: netip.MustParseAddr("192.168.1.1"), GatewayDev: "wan0",
	}}}
	h := HTTP{Applier: a, Lock: core.NewLock(), Events: core.NewEvents()}.Handler()
	body := `{"interface":"wan0","ipv4":{"address":"192.168.1.2/24","gateway":"192.168.1.254"}}`
	for _, query := range []string{"", "?dry_run=true", "?confirm=true"} {
		before, _ := os.ReadFile(a.Store.Path())
		r := httptest.NewRequest("PUT", "/uplink"+query, strings.NewReader(body))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		want := 409
		if query != "" {
			want = 200
		}
		if w.Code != want {
			t.Fatal(w.Code, w.Body)
		}
		if query != "?confirm=true" {
			after, _ := os.ReadFile(a.Store.Path())
			if !bytes.Equal(before, after) {
				t.Fatal("held route change stored intent")
			}
		}
	}
}
