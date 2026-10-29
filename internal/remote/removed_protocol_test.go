package remote

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

func TestRemovedProxyCannotBeEnabledThroughHTTP(t *testing.T) {
	h, _ := testProxyHTTP(t)
	for _, route := range []struct{ method, path string }{
		{"GET", "/shadowsocks/config"}, {"PATCH", "/shadowsocks/config"},
		{"GET", "/shadowsocks/link"}, {"GET", "/shadowsocks/status"},
		{"POST", "/shadowsocks/apply"}, {"POST", "/shadowsocks/plan"},
	} {
		if w := do(t, h, route.method, route.path, ""); w.Code != http.StatusNotFound {
			t.Errorf("%s %s = %d, want 404", route.method, route.path, w.Code)
		}
	}
}

func TestLegacyProxyConfigDoesNotExposeSecretsOrLoseOtherAccess(t *testing.T) {
	var doc core.Document
	doc.Set(ModuleName, []byte(`{"endpoint":"home.example.net","wireguard":{"enabled":true},"socks5":{"enabled":true,"password":"socks-secret"},"shadowsocks":{"enabled":true,"password":"retired-secret"}}`))
	cfg, err := FromDocument(doc)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.WireGuard.Enabled || !cfg.Socks.Enabled || cfg.Socks.Password != "socks-secret" {
		t.Fatalf("remaining access lost: %+v", cfg.Redacted())
	}
	data, err := MarshalConfig(cfg.Redacted())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "shadowsocks") || strings.Contains(string(data), "secret") {
		t.Fatalf("retired config or credentials leaked: %s", data)
	}
}

func TestSocksApplyLeavesLegacyAndUnownedFilesAlone(t *testing.T) {
	_, proxy := testProxyHTTP(t)
	dir := filepath.Dir(proxy.Paths.SocksConf)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(dir, "shadowsocks.json")
	if err := os.WriteFile(legacy, []byte("legacy credential"), 0600); err != nil {
		t.Fatal(err)
	}
	plan, _, err := proxy.Plan(context.Background(), Config{})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Empty() {
		t.Fatalf("SOCKS5 planned changes to an unowned file: %+v", plan)
	}
}

func TestEndpointChangeStillProtectsIssuedSocksLinks(t *testing.T) {
	previous := Config{Endpoint: "old.example.net", Socks: Socks5{Enabled: true, Password: "existing-secret"}}
	next := previous.Clone()
	next.Endpoint = "new.example.net"
	impact, reasons := classifyEndpoint(previous, next)
	if impact != ImpactDisruptive || len(reasons) == 0 {
		t.Fatalf("issued SOCKS5 links not protected: %s %v", impact, reasons)
	}
}
