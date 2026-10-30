package gateway

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/brandlogo"
	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/ingress"
)

func TestCommonLatencyCatalogIcons(t *testing.T) {
	want := map[string]string{
		"WeChat": "thesvg:wechat", "QQ": "thesvg:qq", "Telegram": "thesvg:telegram", "Feishu": "",
		"Weibo": "thesvg:sina-weibo", "Xiaohongshu": "thesvg:xiaohongshu", "X": "thesvg:x", "Douban": "thesvg:douban",
		"Douyin": "", "Bilibili": "thesvg:bilibili", "YouTube": "thesvg:youtube", "Netflix": "thesvg:netflix", "Spotify": "thesvg:spotify",
		"Sina News": "", "Tencent News": "", "NetEase News": "", "CNN": "thesvg:cnn",
		"Taobao": "thesvg:taobao", "JD": "", "Pinduoduo": "", "Amazon": "thesvg:amazon",
		"Baidu": "thesvg:baidu", "Google": "thesvg:google", "GitHub": "thesvg:github", "Zhihu": "thesvg:zhihu",
	}
	catalog := latencyCatalog()
	if len(catalog) != len(want) {
		t.Fatalf("catalog has %d sites, want %d", len(catalog), len(want))
	}
	for _, site := range catalog {
		icon, ok := want[site.Name]
		if !ok || site.Icon != icon {
			t.Errorf("%s: icon = %q, want %q (known site: %v)", site.Name, site.Icon, icon, ok)
		}
		if site.Icon != "" && brandlogo.Built() {
			data, kind, err := ingress.CustomIcon(site.Icon)
			if err != nil || kind != "image/svg+xml" || len(data) == 0 {
				t.Errorf("%s: bundled icon = %q (%d bytes), %v", site.Name, kind, len(data), err)
			}
		}
	}
}

func TestCommonLatencyIconsPropagateThroughMeasurementsAndOverrides(t *testing.T) {
	store := core.NewStore(filepath.Join(t.TempDir(), "olr.json"), ModuleName)
	m, err := OpenCustomLatencyConfig(store)
	if err != nil {
		t.Fatal(err)
	}
	check := func(measured bool) {
		t.Helper()
		for _, site := range latencyCatalog() {
			got := latencySite(m.Snapshot(), site.Name)
			if got == nil || got.Icon != site.Icon || !got.Builtin || got.Category != site.Category {
				t.Fatalf("%s: snapshot = %+v, want catalog icon %q", site.Name, got, site.Icon)
			}
			if measured && (got.Milliseconds == nil || *got.Milliseconds != 12) {
				t.Fatalf("%s: missing measurement: %+v", site.Name, got)
			}
		}
	}
	check(false)
	m.ValidateExit = func(string) error { return nil }
	if err := m.Replace([]CustomLatencySite{{Name: "Google", URL: "https://other.example/", Exit: "Proxy", Icon: "thesvg:qq"}}); err != nil {
		t.Fatal(err)
	}
	check(false)
	if got := latencySite(m.Snapshot(), "Google"); got.Exit != "Proxy" || got.URL != "https://www.gstatic.com/" {
		t.Fatalf("catalog override = %+v", got)
	}
	m.probe = func(context.Context, string) (float64, error) { return 12, nil }
	m.Resolve = func(string) (uint32, error) { return 1, nil }
	m.probeThrough = func(context.Context, string, uint32) (float64, error) { return 12, nil }
	m.sample(context.Background())
	check(true)
}
