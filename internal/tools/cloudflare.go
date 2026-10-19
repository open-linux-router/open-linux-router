package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Run the upstream Rust implementation rather than maintaining a second
// throughput algorithm. Only default-route measurements are supported here.
func runCloudflare(ctx context.Context) (SpeedResult, error) {
	binary := core.LookTool("cloudflare-speed-cli")
	if binary == "" {
		return SpeedResult{}, errors.New("cloudflare-speed-cli is not installed; install v1.0.10 or later from github.com/kavehtehrani/cloudflare-speed-cli/releases")
	}
	cmd := exec.CommandContext(ctx, binary, "--json", "--auto-save", "false",
		"--download-duration", "10s", "--upload-duration", "10s")
	cmd.WaitDelay = time.Second
	// Proxy variables would change the measured route. Do not let daemon
	// environment settings silently select a different network path.
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		switch strings.ToLower(key) {
		case "http_proxy", "https_proxy", "all_proxy":
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	var stdout, stderr limitedBuffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return SpeedResult{}, ctx.Err()
	}
	if stdout.truncated || stderr.truncated {
		return SpeedResult{}, errors.New("cloudflare-speed-cli output exceeded 64 KiB")
	}
	if err != nil {
		return SpeedResult{}, fmt.Errorf("cloudflare-speed-cli failed: %s: %w", strings.TrimSpace(stderr.String()), err)
	}
	return parseCloudflare([]byte(stdout.String()))
}

func parseCloudflare(data []byte) (SpeedResult, error) {
	var result struct {
		Download struct {
			Mbps *float64 `json:"mbps"`
		} `json:"download"`
		Upload struct {
			Mbps *float64 `json:"mbps"`
		} `json:"upload"`
		Idle struct {
			Mean   *float64 `json:"mean_ms"`
			Jitter *float64 `json:"jitter_ms"`
		} `json:"idle_latency"`
		Colo string `json:"colo"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return SpeedResult{}, fmt.Errorf("invalid cloudflare-speed-cli JSON: %w", err)
	}
	if result.Download.Mbps == nil || result.Upload.Mbps == nil ||
		*result.Download.Mbps <= 0 || *result.Upload.Mbps <= 0 {
		return SpeedResult{}, errors.New("cloudflare-speed-cli returned no usable download or upload measurement")
	}
	speed := SpeedResult{Server: "Cloudflare", Location: result.Colo,
		Download: *result.Download.Mbps, Upload: *result.Upload.Mbps}
	if result.Idle.Mean != nil {
		speed.Latency = *result.Idle.Mean
	}
	if result.Idle.Jitter != nil {
		speed.Jitter = *result.Idle.Jitter
	}
	return speed, nil
}
