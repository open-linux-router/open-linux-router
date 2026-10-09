package tools

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestValidTraceTarget(t *testing.T) {
	for _, target := range []string{"example.com", "example.com.", "localhost", "1.1.1.1", "2001:db8::1"} {
		if !validTraceTarget(target) {
			t.Errorf("rejected %q", target)
		}
	}
	for _, target := range []string{" example.com", "example.com ", "-h", "a..b", "example.com/abc", "fe80::1%eth0"} {
		if validTraceTarget(target) {
			t.Errorf("accepted %q", target)
		}
	}
}

func TestRunTraceUsesBoundedTraditionalMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	dir := t.TempDir()
	binary := filepath.Join(dir, "nexttrace")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	result, err := runTrace(context.Background(), "example.com")
	if err != nil {
		t.Fatal(err)
	}
	want := "--table\n--no-color\n--language\nen\n--max-hops\n20\n--queries\n2\n--timeout\n1000\nexample.com\n"
	if result.Target != "example.com" || result.Output != want {
		t.Fatalf("result = %+v", result)
	}
}

func TestTraceMapURL(t *testing.T) {
	good := "https://api.nxtrace.org/tracemap/html/c14e439e-3250-5310-8965-42a1e3545266.html"
	viewer := "https://peer.as/trace?nt=c14e439e-3250-5310-8965-42a1e3545266"
	for _, tc := range []struct{ output, want string }{
		{"1  192.0.2.1\nMapTrace URL: " + good + "\n", viewer},
		{"MapTrace URL: https://assets.nxtrace.org/tracemap/c14e439e-3250-5310-8965-42a1e3545266.html", viewer},
		{"1  192.0.2.1\n", ""},
		{"MapTrace URL: https://api.nxtrace.org.evil.test/tracemap/html/id.html", ""},
		{"MapTrace URL: javascript:alert(1)", ""},
		{"MapTrace URL: https://api.nxtrace.org/tracemap/html/id.html?redirect=evil", ""},
		{"MapTrace URL: https://assets.nxtrace.org/tracemap/c14e439e-3250-5310-8965-42a1e3545266.html?redirect=evil", ""},
		{"MapTrace URL: https://assets.nxtrace.org.evil.test/tracemap/c14e439e-3250-5310-8965-42a1e3545266.html", ""},
	} {
		if got := traceMapURL(tc.output); got != tc.want {
			t.Errorf("traceMapURL(%q) = %q, want %q", tc.output, got, tc.want)
		}
	}
}

func TestRunTraceRejectsUsageWithSuccessfulExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	dir := t.TempDir()
	binary := filepath.Join(dir, "nexttrace")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\necho 'unknown arguments 1.1.1.1'\necho 'usage: nexttrace [-h|--help]'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	_, err := runTrace(context.Background(), "1.1.1.1")
	if err == nil || !strings.Contains(err.Error(), "rejected the trace arguments") {
		t.Fatalf("err = %v", err)
	}
}
