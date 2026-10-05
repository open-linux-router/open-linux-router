package system

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadMetrics(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{"uptime": "123.45 10\n", "meminfo": "MemTotal: 16384 kB\nMemAvailable: 12288 kB\n", "stat": "cpu 1 2 3 4\ncpu0 1 2 3 4\ncpu1 1 2 3 4\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	got, err := readMetrics(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.UptimeSeconds != 123 || got.MemoryTotal != 16384*1024 || got.MemoryUsed != 4096*1024 || got.CPUCores != 2 {
		t.Fatalf("unexpected metrics: %+v", got)
	}
}
