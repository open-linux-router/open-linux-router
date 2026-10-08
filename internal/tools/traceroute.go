package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

const traceTimeout = 75 * time.Second
const traceOutputLimit = 64 << 10

type TraceResult struct {
	Target string `json:"target"`
	Output string `json:"output"`
}

func validTraceTarget(target string) bool {
	if len(target) == 0 || len(target) > 253 || strings.TrimSpace(target) != target {
		return false
	}
	if net.ParseIP(target) != nil {
		return true
	}
	if strings.HasSuffix(target, ".") {
		target = strings.TrimSuffix(target, ".")
	}
	if len(target) == 0 {
		return false
	}
	for _, label := range strings.Split(target, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, ch := range label {
			if ch > 127 || !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-') {
				return false
			}
		}
	}
	return true
}

func runTrace(ctx context.Context, target string) (TraceResult, error) {
	binary := core.LookTool("nexttrace")
	if binary == "" {
		return TraceResult{}, errors.New("nexttrace is not installed on the router; install it from github.com/nxtrace/NTrace-core")
	}
	// Explicit traditional mode protects this finite request from upstream's planned MTR default.
	cmd := exec.CommandContext(ctx, binary, "--traceroute", "--table", "--no-color", "--language", "en",
		"--max-hops", "20", "--queries", "2", "--timeout", "1000", target)
	cmd.WaitDelay = time.Second
	var stdout, stderr limitedBuffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return TraceResult{}, ctx.Err()
	}
	if stdout.truncated || stderr.truncated {
		return TraceResult{}, errors.New("nexttrace output exceeded 64 KiB")
	}
	if err != nil {
		return TraceResult{}, fmt.Errorf("nexttrace failed: %s: %w", strings.TrimSpace(stderr.String()), err)
	}
	if stdout.Len() == 0 {
		return TraceResult{}, errors.New("nexttrace returned no route")
	}
	return TraceResult{Target: target, Output: stdout.String()}, nil
}

type limitedBuffer struct {
	bytes.Buffer
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if remaining := traceOutputLimit - b.Len(); remaining < n {
		b.truncated = true
		if remaining > 0 {
			_, _ = b.Buffer.Write(p[:remaining])
		}
		return n, nil
	}
	_, _ = b.Buffer.Write(p)
	return n, nil
}
