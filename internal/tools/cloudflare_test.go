package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

const cloudflareFixture = `{"download":{"mbps":150},"upload":{"mbps":30},"idle_latency":{"mean_ms":12,"jitter_ms":2},"colo":"LHR","future_field":true}`

func TestCloudflareResultAndScore(t *testing.T) {
	speed, err := parseCloudflare([]byte(cloudflareFixture))
	if err != nil || speed.Server != "Cloudflare" || speed.Location != "LHR" || speed.Latency != 12 || speed.Jitter != 2 {
		t.Fatalf("result = %+v, error = %v", speed, err)
	}
	categories, overall := scoreQuality(QualitySnapshot{Speed: &speed}, time.Now())
	want := 90*.7 + 80*.3
	if categories[0].Score == nil || *categories[0].Score != want || overall.Score == nil || *overall.Score != want {
		t.Fatalf("Cloudflare throughput did not contribute to score: %+v, %+v", categories[0], overall)
	}
	for _, input := range []string{`{}`, `null`, `{"download":{"mbps":100}}`, `{"download":{"mbps":0},"upload":{"mbps":10}}`, `{"download":{"mbps":-1},"upload":{"mbps":10}}`, `{"download":{"mbps":1e999},"upload":{"mbps":10}}`, `not json`} {
		if _, err := parseCloudflare([]byte(input)); err == nil {
			t.Errorf("accepted invalid measurement %s", input)
		}
	}
}

func TestCloudflareCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	dir := t.TempDir()
	binary := filepath.Join(dir, "cloudflare-speed-cli")
	script := "#!/bin/sh\n" +
		"test \"$*\" = '--json --auto-save false --download-duration 10s --upload-duration 10s' || exit 3\n" +
		"test -z \"$HTTP_PROXY$HTTPS_PROXY$ALL_PROXY$http_proxy$https_proxy$all_proxy\" || exit 4\n" +
		"printf '%s' '" + cloudflareFixture + "'\n"
	if err := os.WriteFile(binary, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		t.Setenv(name, "http://proxy.invalid")
	}
	if _, err := runCloudflare(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := runCloudflare(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	if err := os.WriteFile(binary, []byte("#!/bin/sh\necho failure >&2\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := runCloudflare(context.Background()); err == nil {
		t.Fatal("accepted failed subprocess")
	}
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexec /bin/sleep 30\n"), 0755); err != nil {
		t.Fatal(err)
	}
	deadline, stop := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer stop()
	if _, err := runCloudflare(deadline); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("running subprocess deadline: %v", err)
	}
	if err := os.Remove(binary); err != nil {
		t.Fatal(err)
	}
	if _, err := runCloudflare(context.Background()); err == nil {
		t.Fatal("missing CLI must be reported")
	}
	categories, _ := scoreQuality(QualitySnapshot{SpeedError: "CLI unavailable"}, time.Now())
	if categories[0].Score != nil || categories[0].Components[0].Reason != "CLI unavailable" {
		t.Fatal("unavailable CLI must remain unmeasured")
	}
}
