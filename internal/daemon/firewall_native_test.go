package daemon

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/firewall"
)

func TestServiceChangeDoesNotOverwriteNativeFirewall(t *testing.T) {
	store := core.NewStore(filepath.Join(t.TempDir(), "olr.json"), firewall.ModuleName)
	doc := core.Document{}
	raw, err := firewall.MarshalConfig(firewall.Config{Enabled: true, Ruleset: json.RawMessage(`{"nftables":[{"table":{"family":"inet","name":"olr_filter"}}]}`)})
	if err != nil {
		t.Fatal(err)
	}
	doc.Set(firewall.ModuleName, raw)
	if err := store.Save(doc); err != nil {
		t.Fatal(err)
	}
	kernel := &firewall.StaticKernel{State: []string{"externally edited native table"}}
	a := firewall.Applier{Store: store, Kernel: kernel}
	reapplyFirewall(context.Background(), a, core.New(), slog.New(slog.NewTextHandler(io.Discard, nil)), "remote")
	if len(kernel.State) != 1 || kernel.State[0] != "externally edited native table" {
		t.Fatal("service event overwrote native rules")
	}
}
