package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

const traceTimeout = 75 * time.Second
const traceOutputLimit = 64 << 10

type TraceResult struct {
	Target string `json:"target"`
	Output string `json:"output"`
	MapURL string `json:"map_url,omitempty"`
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
	// Table mode selects a finite traditional trace on both older and newer NextTrace.
	cmd := exec.CommandContext(ctx, binary, "--table", "--no-color", "--language", "en",
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
	// Some NextTrace releases print argument errors and usage to stdout with exit 0.
	if strings.HasPrefix(stdout.String(), "unknown arguments ") || strings.HasPrefix(stdout.String(), "usage: nexttrace ") ||
		strings.Contains(stdout.String(), "\nusage: nexttrace ") {
		return TraceResult{}, errors.New("nexttrace rejected the trace arguments; check the installed version")
	}
	return TraceResult{Target: target, Output: stdout.String(), MapURL: traceMapURL(stdout.String())}, nil
}

var traceMapID = regexp.MustCompile(`^[0-9a-fA-F]{8}(-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}$`)

// NextTrace has used multiple map hosts. Normalize their trace IDs to the
// current viewer so the browser can check and embed a single origin.
func traceMapURL(output string) string {
	const marker = "MapTrace URL:"
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, marker) {
			continue
		}
		raw := strings.TrimSpace(strings.TrimPrefix(line, marker))
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
			continue
		}
		var id string
		switch u.Host {
		case "api.nxtrace.org":
			id = strings.TrimSuffix(strings.TrimPrefix(u.Path, "/tracemap/html/"), ".html")
			if u.Path != "/tracemap/html/"+id+".html" {
				continue
			}
		case "assets.nxtrace.org":
			id = strings.TrimSuffix(strings.TrimPrefix(u.Path, "/tracemap/"), ".html")
			if u.Path != "/tracemap/"+id+".html" {
				continue
			}
		default:
			continue
		}
		if traceMapID.MatchString(id) {
			return "https://peer.as/trace?nt=" + id
		}
	}
	return ""
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
