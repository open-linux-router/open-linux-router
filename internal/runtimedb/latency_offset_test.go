package runtimedb

import (
	"path/filepath"
	"testing"
	"time"
)

func TestLatencySamplePreservesBucketOffsetOnRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	hour := time.Date(2026, 4, 5, 1, 0, 0, 0, time.FixedZone("", 10*3600+30*60))
	started := hour.Add(45 * time.Minute).UTC()
	value := 12.0
	if err := db.SaveLatencySamples([]LatencySample{{URL: "https://example.com/", HourStart: hour, CheckedAt: started, Milliseconds: &value}}, hour.Add(-48*time.Hour), started); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.LatencySamples(hour.Add(-48*time.Hour), started)
	if err != nil || len(rows) != 1 || rows[0].HourStart.Format(time.RFC3339) != hour.Format(time.RFC3339) {
		t.Fatalf("offset restore %+v %v", rows, err)
	}
}
