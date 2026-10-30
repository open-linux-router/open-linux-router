package gateway

import (
	"context"
	"fmt"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/runtimedb"
)

func (m *CustomLatencyMonitor) clock() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

// Freeze the start offset when deriving the calendar label. This keeps HH:00
// and both occurrences distinct even when a DST change repeats half an hour.
func latencyHour(at time.Time) time.Time {
	_, offset := at.Zone()
	return time.Date(at.Year(), at.Month(), at.Day(), at.Hour(), 0, 0, 0, time.FixedZone("", offset))
}

func nextLatencyRound(now time.Time) time.Time {
	// Walk absolute minutes, then inspect local time, so skipped/repeated hours
	// and half-hour daylight-saving transitions never manufacture a local time.
	next := now.Truncate(time.Minute).Add(time.Minute)
	for next.In(time.Local).Minute() != 30 {
		next = next.Add(time.Minute)
	}
	return next
}

func retainedLatencySamples(samples []LatencySample, now time.Time) []LatencySample {
	cutoff := now.Add(-latencyHistoryRetention)
	out := make([]LatencySample, 0, len(samples))
	for _, sample := range samples {
		if !sample.HourStart.Before(cutoff) && !sample.CheckedAt.After(now) {
			out = append(out, sample)
		}
	}
	return out
}

// RestoreHistory attaches durable observation history; configuration is never
// used as a measurement store. The daemon logs errors and keeps live probing.
func (m *CustomLatencyMonitor) RestoreHistory(db *runtimedb.Store) error {
	now := m.clock()
	rows, err := db.LatencySamples(now.Add(-latencyHistoryRetention), now)
	if err != nil {
		return fmt.Errorf("reading latency history: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.db = db
	for _, row := range rows {
		for i := range m.results {
			current := &m.results[i]
			if current.URL != row.URL || current.Exit != row.Exit {
				continue
			}
			current.Samples = append(current.Samples, LatencySample{Milliseconds: row.Milliseconds, Error: row.Error, CheckedAt: row.CheckedAt, HourStart: row.HourStart})
			if !current.CheckedAt.After(row.CheckedAt) {
				current.CheckedAt, current.Milliseconds, current.Error = row.CheckedAt, row.Milliseconds, row.Error
			}
		}
	}
	return nil
}

func (m *CustomLatencyMonitor) saveHistoryLocked() {
	if m.db == nil || len(m.dirty) == 0 {
		return
	}
	now := m.clock()
	cutoff := now.Add(-latencyHistoryRetention)
	// A failed database write retries with the next result. Bound pending memory
	// by the same target/hour identity and retention as the durable table.
	type key struct {
		url, exit string
		hour      int64
	}
	pending := make(map[key]runtimedb.LatencySample)
	for _, row := range m.dirty {
		if row.HourStart.Before(cutoff) || row.CheckedAt.After(now) {
			continue
		}
		k := key{row.URL, row.Exit, row.HourStart.Unix()}
		if old, ok := pending[k]; !ok || !old.CheckedAt.After(row.CheckedAt) {
			pending[k] = row
		}
	}
	m.dirty = m.dirty[:0]
	for _, row := range pending {
		m.dirty = append(m.dirty, row)
	}
	if err := m.db.SaveLatencySamples(m.dirty, cutoff, now); err != nil {
		if m.Log != nil {
			m.Log.Error("could not save latency history", "error", err)
		}
		return
	}
	m.dirty = nil
}

// Run schedules monitored sites independently of Internet/DNS sampling and
// target-selection wakeups. Startup catches up only the current missing hour
// after :30, never fills past hours or overwrites a restored/manual sample.
func (m *CustomLatencyMonitor) Run(ctx context.Context) {
	m.setContext(ctx)
	now := m.clock().In(time.Local)
	if now.Minute() >= 30 {
		m.sampleAt(ctx, now, true)
	}
	defer func() { m.mu.Lock(); defer m.mu.Unlock(); m.saveHistoryLocked() }()
	for ctx.Err() == nil {
		now = m.clock()
		next := nextLatencyRound(now)
		if !m.waitForRound(ctx, next.Sub(now)) {
			return
		}
		m.sampleAt(ctx, m.clock(), false)
	}
}

func (m *CustomLatencyMonitor) waitForRound(ctx context.Context, delay time.Duration) bool {
	if m.wait != nil {
		return m.wait(ctx, delay)
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
