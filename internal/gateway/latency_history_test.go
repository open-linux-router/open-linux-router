package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/runtimedb"
)

func hourlyMonitor(t *testing.T, now *time.Time) *CustomLatencyMonitor {
	t.Helper()
	m, err := NewCustomLatencyMonitor(filepath.Join(t.TempDir(), "sites.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Replace([]CustomLatencySite{{Name: "A", URL: "https://a.example/"}}); err != nil {
		t.Fatal(err)
	}
	m.now = func() time.Time { return *now }
	m.probe = func(context.Context, string) (float64, error) { return 12, nil }
	return m
}

func localZone(t *testing.T, name string) *time.Location {
	t.Helper()
	zone, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	before := time.Local
	time.Local = zone
	t.Cleanup(func() { time.Local = before })
	return zone
}

func TestHourlyLatencyScheduleUsesCalendarHalfHour(t *testing.T) {
	for _, name := range []string{"Asia/Shanghai", "Asia/Kolkata", "Asia/Kathmandu", "America/New_York", "Australia/Lord_Howe"} {
		t.Run(name, func(t *testing.T) {
			zone := localZone(t, name)
			for _, tc := range []struct {
				minute, second int
				delay          time.Duration
			}{{0, 0, 30 * time.Minute}, {29, 59, time.Second}, {30, 0, time.Hour}, {59, 0, 31 * time.Minute}} {
				now := time.Date(2026, 10, 30, 12, tc.minute, tc.second, 0, zone)
				next := nextLatencyRound(now)
				if next.Sub(now) != tc.delay || next.In(zone).Minute() != 30 {
					t.Fatalf("%s next %s delay %v", now, next, next.Sub(now))
				}
				hour := latencyHour(now)
				if hour.Hour() != 12 || hour.Minute() != 0 || hour.Second() != 0 {
					t.Fatalf("bad calendar hour %s", hour)
				}
			}
		})
	}
}

func TestHourlyLatencyDSTHoursRemainDistinct(t *testing.T) {
	zone := localZone(t, "America/New_York")
	first := time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC).In(zone)
	second := first.Add(time.Hour)
	if first.Hour() != 1 || second.Hour() != 1 || latencyHour(first).Equal(latencyHour(second)) {
		t.Fatal("repeated hour collapsed")
	}
	if got := nextLatencyRound(first); !got.Equal(second) {
		t.Fatalf("next repeated hour=%s", got)
	}
	spring := time.Date(2026, 3, 8, 6, 30, 0, 0, time.UTC).In(zone)
	if got := nextLatencyRound(spring).In(zone); got.Hour() != 3 || got.Minute() != 30 {
		t.Fatalf("spring next %s", got)
	}
}

func TestHourlyLatencyStartupCatchupAndInternetWakeIndependence(t *testing.T) {
	localZone(t, "Asia/Shanghai")
	now := time.Date(2026, 10, 30, 12, 29, 0, 0, time.Local)
	m := hourlyMonitor(t, &now)
	calls := 0
	m.probe = func(context.Context, string) (float64, error) { calls++; return float64(calls), nil }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m.Run(ctx)
	if calls != 0 {
		t.Fatal("startup before half hour probed")
	}
	now = now.Add(2 * time.Minute)
	m.sampleAt(context.Background(), now, true)
	m.sampleAt(context.Background(), now.Add(time.Minute), true)
	if calls != 1 {
		t.Fatalf("catchup retried measured current hour %d times", calls)
	}
	now = now.Add(time.Hour)
	m.sampleAt(context.Background(), now, true)
	if calls != 2 || len(m.Snapshot()[0].Samples) != 2 {
		t.Fatal("new hour did not probe")
	}
	internet := NewLatencyMonitor()
	internet.Custom = m
	internet.dnsProbe = nil
	internet.probe = func(context.Context, string) (float64, error) { return 2, nil }
	for range 3 {
		internet.sample(context.Background())
	}
	if calls != 2 {
		t.Fatal("Internet sampling triggered monitored site probes")
	}
}

func TestHourlyLatencyStartBoundaryMergeFailureAndCancellation(t *testing.T) {
	localZone(t, "Asia/Kathmandu")
	now := time.Date(2026, 10, 31, 0, 0, 5, 0, time.Local)
	m := hourlyMonitor(t, &now)
	site := m.allSitesLocked()[0]
	old := now.Add(-10 * time.Second)
	// Completion after midnight still writes the prior day's 23:00 bucket.
	m.probeBackground(context.Background(), site, old)
	got := m.Snapshot()[0]
	if len(got.Samples) != 1 || got.Samples[0].HourStart.Day() != 30 || got.Samples[0].HourStart.Hour() != 23 || !got.Samples[0].CheckedAt.Equal(old) {
		t.Fatalf("boundary: %+v", got)
	}
	m.probeBackground(context.Background(), site, now)
	m.probe = func(context.Context, string) (float64, error) { return 0, errors.New("offline") }
	now = now.Add(time.Minute)
	m.probeOne(context.Background(), site, now)
	got = m.Snapshot()[0]
	if len(got.Samples) != 2 || got.Milliseconds != nil || got.Error != "Connection failed" || got.Samples[1].Error != "Connection failed" {
		t.Fatalf("manual failure: %+v", got)
	}
	// An older attempt finishing later cannot restore the success or append a slot.
	m.mu.Lock()
	value := 99.0
	m.storeResult(LatencySite{URL: site.URL, CheckedAt: now.Add(-time.Second), Milliseconds: &value})
	m.mu.Unlock()
	if got = m.Snapshot()[0]; got.Milliseconds != nil || len(got.Samples) != 2 {
		t.Fatalf("old completion overwrote failure: %+v", got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m.probeOne(ctx, site, now.Add(time.Minute))
	if got = m.Snapshot()[0]; !got.CheckedAt.Equal(now) || len(got.Samples) != 2 {
		t.Fatal("shutdown recorded failed sample")
	}
	body, err := json.Marshal(LatencySnapshot{LocalTime: now, Sites: []LatencySite{}, Custom: m.Snapshot()})
	if err != nil || !strings.Contains(string(body), `"hour_start":"2026-10-31T00:00:00+05:45"`) || !strings.Contains(string(body), `"local_time":"2026-10-31T00:01:05+05:45"`) {
		t.Fatalf("offset API: %s %v", body, err)
	}
	snapshot := m.Snapshot()
	snapshot[0].Samples[0].Error = "changed"
	if m.Snapshot()[0].Samples[0].Error == "changed" {
		t.Fatal("history aliases snapshot")
	}
}

func TestHourlyLatencyDurableRestartAndTargetIdentity(t *testing.T) {
	localZone(t, "Asia/Shanghai")
	path := filepath.Join(t.TempDir(), "runtime.db")
	db, err := runtimedb.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 30, 12, 31, 0, 0, time.Local)
	store := core.NewStore(filepath.Join(t.TempDir(), "olr.json"), ModuleName)
	m, err := OpenCustomLatencyConfig(store)
	if err != nil {
		t.Fatal(err)
	}
	m.now = func() time.Time { return now }
	if err := m.Replace([]CustomLatencySite{{Name: "A", URL: "https://a.example/"}}); err != nil {
		t.Fatal(err)
	}
	if err := m.RestoreHistory(db); err != nil {
		t.Fatal(err)
	}
	m.probe = func(context.Context, string) (float64, error) { return 15, nil }
	m.sample(context.Background())
	now = now.Add(time.Hour)
	m.probe = func(context.Context, string) (float64, error) { return 0, errors.New("offline") }
	m.sample(context.Background())
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = runtimedb.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	restored, err := OpenCustomLatencyConfig(store)
	if err != nil {
		t.Fatal(err)
	}
	restored.now = func() time.Time { return now }
	if err := restored.RestoreHistory(db); err != nil {
		t.Fatal(err)
	}
	for _, site := range restored.Snapshot() {
		if len(site.Samples) != 2 || site.Error != "Connection failed" {
			t.Fatalf("restored %s: %+v", site.Name, site)
		}
	}
	calls := 0
	restored.probe = func(context.Context, string) (float64, error) { calls++; return 2, nil }
	restored.sampleAt(context.Background(), now, true)
	if calls != 0 {
		t.Fatal("startup overwrote restored hour")
	}
	if err := restored.Replace([]CustomLatencySite{{Name: "Renamed", URL: "https://a.example/"}}); err != nil {
		t.Fatal(err)
	}
	if got := latencySite(restored.Snapshot(), "Renamed"); got == nil || len(got.Samples) != 2 {
		t.Fatal("rename lost history")
	}
	restored.ValidateExit = func(string) error { return nil }
	if err := restored.Replace([]CustomLatencySite{{Name: "Renamed", URL: "https://a.example/", Exit: "Proxy"}}); err != nil {
		t.Fatal(err)
	}
	if got := latencySite(restored.Snapshot(), "Renamed"); len(got.Samples) != 0 || !got.CheckedAt.IsZero() {
		t.Fatal("exit change retained old route measurements")
	}
}

func TestHourlyLatencyRetentionAndFailedCheckpointRetry(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 31, 0, 0, time.Local)
	m := hourlyMonitor(t, &now)
	for i := 50; i >= 0; i-- {
		m.probeBackground(context.Background(), m.allSitesLocked()[0], now.Add(-time.Duration(i)*time.Hour))
	}
	got := m.Snapshot()[0]
	if len(got.Samples) != 48 {
		t.Fatalf("retained %d hours", len(got.Samples))
	}
	for i := 1; i < len(got.Samples); i++ {
		if !got.Samples[i].HourStart.After(got.Samples[i-1].HourStart) {
			t.Fatal("history not chronological")
		}
	}
	path := filepath.Join(t.TempDir(), "runtime.db")
	db, err := runtimedb.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	m.db = db
	db.Close()
	now = now.Add(time.Minute)
	m.sample(context.Background())
	if len(m.dirty) != 1 || m.Snapshot()[0].Milliseconds == nil {
		t.Fatal("failed checkpoint discarded live result or retry")
	}
	db, err = runtimedb.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m.db = db
	m.mu.Lock()
	m.saveHistoryLocked()
	m.mu.Unlock()
	rows, err := db.LatencySamples(now.Add(-latencyHistoryRetention), now)
	if err != nil || len(rows) != 1 || len(m.dirty) != 0 {
		t.Fatalf("retry %v %d pending %d", err, len(rows), len(m.dirty))
	}
}

func TestHourlyLatencyRunWaitsUntilHalfHourAndRoundsEveryHour(t *testing.T) {
	localZone(t, "Asia/Shanghai")
	now := time.Date(2026, 10, 30, 12, 29, 0, 0, time.Local)
	m := hourlyMonitor(t, &now)
	calls := 0
	m.probe = func(context.Context, string) (float64, error) { calls++; return 3, nil }
	waits := []time.Duration{}
	m.wait = func(_ context.Context, delay time.Duration) bool {
		waits = append(waits, delay)
		if len(waits) > 2 {
			return false
		}
		now = now.Add(delay)
		return true
	}
	m.Run(context.Background())
	if calls != 2 || len(waits) != 3 || waits[0] != time.Minute || waits[1] != time.Hour || waits[2] != time.Hour {
		t.Fatalf("calls=%d waits=%v", calls, waits)
	}
	if got := m.Snapshot()[0]; len(got.Samples) != 2 || got.Samples[0].CheckedAt.In(time.Local).Minute() != 30 || got.Samples[1].HourStart.Hour() != 13 {
		t.Fatalf("schedule samples=%+v", got)
	}
	// Restart at an arbitrary minute must preserve a restored/manual current hour.
	now = now.Add(10 * time.Minute)
	m.wait = func(_ context.Context, delay time.Duration) bool {
		if delay != 50*time.Minute {
			t.Errorf("restart next round=%v", delay)
		}
		return false
	}
	m.Run(context.Background())
	if calls != 2 {
		t.Fatal("arbitrary restart remeasured current hour")
	}
}

func TestHourlyLatencyHalfHourDSTBucketKeepsStartOffset(t *testing.T) {
	zone := localZone(t, "Australia/Lord_Howe")
	// 01:45 repeats when the offset changes from +11 to +10:30.
	first := time.Date(2026, 4, 4, 14, 45, 0, 0, time.UTC).In(zone)
	second := first.Add(30 * time.Minute)
	for _, at := range []time.Time{first, second} {
		hour := latencyHour(at)
		_, want := at.Zone()
		_, got := hour.Zone()
		if hour.Hour() != 1 || hour.Minute() != 0 || want != got {
			t.Fatalf("bucket %s for %s", hour, at)
		}
	}
	if latencyHour(first).Equal(latencyHour(second)) {
		t.Fatal("repeated partial hour collapsed")
	}
}
