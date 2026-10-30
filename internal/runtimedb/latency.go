package runtimedb

import (
	"database/sql"
	"fmt"
	"time"
)

func createLatencySamples(tx *sql.Tx) error {
	_, err := tx.Exec(`CREATE TABLE latency_samples (url TEXT NOT NULL, exit_name TEXT NOT NULL, hour_start INTEGER NOT NULL, hour_offset INTEGER NOT NULL, checked_at INTEGER NOT NULL, milliseconds REAL, error TEXT NOT NULL, PRIMARY KEY(url,exit_name,hour_start)); CREATE INDEX latency_samples_time ON latency_samples(hour_start); PRAGMA user_version=4;`)
	return err
}

// LatencySample stores one target's latest attempt in a router calendar hour.
// Bucket instants are absolute; the gateway renders them in the router's zone.
type LatencySample struct {
	URL, Exit    string
	HourStart    time.Time
	CheckedAt    time.Time
	Milliseconds *float64
	Error        string
}

const LatencySampleLimit = 10_000

func (s *Store) SaveLatencySamples(samples []LatencySample, cutoff, now time.Time) error {
	return s.transaction(func(tx *sql.Tx) error {
		if _, err := tx.Exec("DELETE FROM latency_samples WHERE hour_start < ? OR hour_start > ?", cutoff.Unix(), now.Unix()); err != nil {
			return err
		}
		for _, sample := range samples {
			if sample.HourStart.Before(cutoff) || sample.HourStart.After(now) {
				continue
			}
			_, offset := sample.HourStart.Zone()
			_, err := tx.Exec(`INSERT INTO latency_samples(url,exit_name,hour_start,hour_offset,checked_at,milliseconds,error) VALUES(?,?,?,?,?,?,?) ON CONFLICT(url,exit_name,hour_start) DO UPDATE SET hour_offset=excluded.hour_offset,checked_at=excluded.checked_at,milliseconds=excluded.milliseconds,error=excluded.error WHERE excluded.checked_at >= latency_samples.checked_at`, sample.URL, sample.Exit, sample.HourStart.Unix(), offset, sample.CheckedAt.UnixNano(), sample.Milliseconds, sample.Error)
			if err != nil {
				return err
			}
		}
		// Changed/removed targets are retained only within the same bounded window.
		_, err := tx.Exec("DELETE FROM latency_samples WHERE rowid IN (SELECT rowid FROM latency_samples ORDER BY checked_at DESC LIMIT -1 OFFSET ?)", LatencySampleLimit)
		return err
	})
}

func (s *Store) LatencySamples(cutoff, now time.Time) ([]LatencySample, error) {
	rows, err := s.db.Query("SELECT url,exit_name,hour_start,hour_offset,checked_at,milliseconds,error FROM latency_samples WHERE hour_start>=? AND hour_start<=? ORDER BY hour_start,checked_at LIMIT ?", cutoff.Unix(), now.Unix(), LatencySampleLimit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	samples := []LatencySample{}
	for rows.Next() {
		var sample LatencySample
		var hour, at int64
		var offset int
		var ms sql.NullFloat64
		if err := rows.Scan(&sample.URL, &sample.Exit, &hour, &offset, &at, &ms, &sample.Error); err != nil {
			return nil, err
		}
		sample.HourStart = time.Unix(hour, 0).In(time.FixedZone("", offset))
		sample.CheckedAt = time.Unix(0, at).UTC()
		if ms.Valid {
			sample.Milliseconds = &ms.Float64
		}
		samples = append(samples, sample)
	}
	if len(samples) > LatencySampleLimit {
		return nil, fmt.Errorf("latency history exceeds entry limit")
	}
	return samples, rows.Err()
}
