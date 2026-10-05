package system

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

func TestReadMetricsRejectsIncompleteData(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{"uptime": "", "meminfo": "MemTotal: 100 kB\nMemAvailable: 200 kB\n", "stat": "cpu 1 2 3 4\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := readMetrics(dir); err == nil {
		t.Fatal("empty uptime accepted")
	}
	if err := os.WriteFile(filepath.Join(dir, "uptime"), []byte("12 0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readMetrics(dir); err == nil {
		t.Fatal("invalid memory accepted")
	}
}

func TestReadCPU(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stat")
	if err := os.WriteFile(path, []byte("cpu 10 2 3 20 5 0 0 0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := readCPU(path)
	if err != nil || got.total != 40 || got.idle != 25 {
		t.Fatalf("sample = %+v, %v", got, err)
	}
}

func TestMetricsEndpoint(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{"uptime": "86461.5 0\n", "meminfo": "MemTotal: 16000 kB\nMemAvailable: 8000 kB\n", "stat": "cpu 100 0 0 100 0 0 0 0\ncpu0 50 0 0 50\ncpu1 50 0 0 50\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	rec := httptest.NewRecorder()
	HTTP{Proc: dir}.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var got Metrics
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.UptimeSeconds != 86461 || got.CPUCores != 2 || got.MemoryUsed != 8000*1024 {
		t.Fatalf("metrics: %+v", got)
	}
}
