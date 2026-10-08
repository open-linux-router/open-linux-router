package tools

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
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
	want := "--traceroute\n--table\n--no-color\n--language\nen\n--max-hops\n20\n--queries\n2\n--timeout\n1000\nexample.com\n"
	if result.Target != "example.com" || result.Output != want {
		t.Fatalf("result = %+v", result)
	}
}
