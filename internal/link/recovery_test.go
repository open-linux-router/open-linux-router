package link

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

func TestRecoverRetiresMovedTrialNetwork(t *testing.T) {
	a, writer := removalApplier(t)
	trial := lanOn("lan1")
	if err := a.Recover(context.Background(), trial); err != nil {
		t.Fatal(err)
	}
	var restored, removed bool
	for _, want := range writer.Applied {
		if want.Interface == "lan0" && len(want.Addrs) > 0 {
			restored = true
		}
		if want.Interface == "lan1" && slices.Contains(want.Retire, trial.Networks[0].IPv4.RouterPrefix()) {
			removed = true
		}
	}
	if !restored || !removed {
		t.Fatalf("incomplete recovery: %+v", writer.Applied)
	}
}

func TestHTTPDisruptiveChangeJournalsBeforeApply(t *testing.T) {
	a, writer := removalApplier(t)
	srv := core.New()
	r := &core.Recovery{Store: a.Store, Lock: srv.ApplyLock(), Path: filepath.Join(t.TempDir(), "journal.json")}
	srv.Recovery = r
	srv.Mount("link", HTTP{Applier: a, Lock: srv.ApplyLock(), Events: srv.Events()}.Routes(), Config{})
	cfg, _ := a.Load()
	next := cfg.Networks[0].IPv4.Router.Next()
	cfg.Networks[0].IPv4.Router = &next
	body, _ := json.Marshal(cfg)
	for _, query := range []string{"?dry_run=true", "", "?confirm=true"} {
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, httptest.NewRequest("PUT", "/api/link/config"+query, strings.NewReader(string(body))))
		if query == "?confirm=true" {
			if w.Code != 200 || r.View().State != "pending" || len(writer.Applied) == 0 {
				t.Fatal(w.Code, w.Body, r.View())
			}
			if _, err := os.Stat(r.Path); err != nil {
				t.Fatal(err)
			}
		} else if r.View().State != "idle" || len(writer.Applied) != 0 {
			t.Fatal("preview changed runtime or created a trial")
		}
	}
}
