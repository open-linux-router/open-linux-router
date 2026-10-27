package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// fastDuration is the cap fast-cli is given. Its own stable-speed stop usually
// finishes sooner, and the tool accepts 7 to 30 seconds.
const fastDuration = 10

// runFast measures the Netflix Open Connect path through the upstream Zig
// tool. Owning no measurement code is the same bargain as the Cloudflare
// reference transfer: the web page's own client is the thing being borrowed.
func runFast(ctx context.Context, mark uint32) (SpeedResult, error) {
	binary := core.LookTool("fast-cli")
	if binary == "" {
		return SpeedResult{}, errors.New("fast-cli is not installed; install v0.3.10 or later from github.com/mikkelam/fast-cli/releases")
	}
	cmd := exec.CommandContext(ctx, binary, "-j", "-u", "-d", strconv.Itoa(fastDuration))
	cmd.WaitDelay = time.Second
	cmd.Env = commandEnv()
	cleanup, err := markCommand(cmd, mark)
	if err != nil {
		return SpeedResult{}, err
	}
	defer cleanup()
	var stdout, stderr limitedBuffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	if ctx.Err() != nil {
		return SpeedResult{}, ctx.Err()
	}
	if stdout.truncated || stderr.truncated {
		return SpeedResult{}, errors.New("fast-cli output exceeded 64 KiB")
	}
	if err != nil {
		return SpeedResult{}, fmt.Errorf("fast-cli failed: %s: %w", strings.TrimSpace(stderr.String()), err)
	}
	return parseFast([]byte(stdout.String()))
}

func parseFast(data []byte) (SpeedResult, error) {
	var result struct {
		Download *float64 `json:"download_mbps"`
		Upload   *float64 `json:"upload_mbps"`
		Ping     *float64 `json:"ping_ms"`
		Error    *string  `json:"error"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return SpeedResult{}, fmt.Errorf("invalid fast-cli JSON: %w", err)
	}
	if result.Error != nil && *result.Error != "" {
		return SpeedResult{}, fmt.Errorf("fast-cli reported: %s", *result.Error)
	}
	if result.Download == nil || *result.Download <= 0 ||
		result.Upload == nil || *result.Upload <= 0 {
		return SpeedResult{}, errors.New("fast-cli returned no usable download or upload measurement")
	}
	speed := SpeedResult{
		Server: "fast.com", Location: "Netflix",
		Download: *result.Download, Upload: *result.Upload,
	}
	if result.Ping != nil {
		speed.Latency = *result.Ping
	}
	return speed, nil
}
