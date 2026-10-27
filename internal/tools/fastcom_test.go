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

const fastFixture = `{"download_mbps": 131.5, "ping_ms": 20.8, "upload_mbps": 62, "error": null}`

func TestFastResult(t *testing.T) {
	speed, err := parseFast([]byte(fastFixture))
	if err != nil || speed.Server != "fast.com" || speed.Location != "Netflix" ||
		speed.Download != 131.5 || speed.Upload != 62 || speed.Latency != 20.8 || speed.Jitter != nil {
		t.Fatalf("result = %+v, error = %v", speed, err)
	}
	for _, input := range []string{
		`{}`,
		`null`,
		`{"download_mbps": 100}`,
		`{"download_mbps": 100, "upload_mbps": 0}`,
		`{"download_mbps": -1, "upload_mbps": 10}`,
		`{"download_mbps": 100, "upload_mbps": 10, "error": "the request was blocked"}`,
		`not json`,
	} {
		if _, err := parseFast([]byte(input)); err == nil {
			t.Errorf("accepted invalid measurement %s", input)
		}
	}
}

func TestFastCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	dir := t.TempDir()
	binary := filepath.Join(dir, "fast-cli")
	script := "#!/bin/sh\n" +
		"test \"$*\" = '-j -u -d 10' || exit 3\n" +
		"test -z \"$HTTP_PROXY$HTTPS_PROXY$ALL_PROXY$http_proxy$https_proxy$all_proxy\" || exit 4\n" +
		"printf '%s' '" + fastFixture + "'\n"
	if err := os.WriteFile(binary, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		t.Setenv(name, "http://proxy.invalid")
	}
	if _, err := runFast(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := runFast(ctx, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	if err := os.WriteFile(binary, []byte("#!/bin/sh\necho failure >&2\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := runFast(context.Background(), 0); err == nil {
		t.Fatal("accepted failed subprocess")
	}
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexec /bin/sleep 30\n"), 0755); err != nil {
		t.Fatal(err)
	}
	deadline, stop := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer stop()
	if _, err := runFast(deadline, 0); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("running subprocess deadline: %v", err)
	}
	if err := os.Remove(binary); err != nil {
		t.Fatal(err)
	}
	if _, err := runFast(context.Background(), 0); err == nil {
		t.Fatal("missing CLI must be reported")
	}
}
