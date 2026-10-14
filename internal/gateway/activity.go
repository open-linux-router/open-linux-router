package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"sync"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

type activityBytes struct {
	Up   uint64 `json:"up"`
	Down uint64 `json:"down"`
}
type activityBucket struct {
	At    time.Time     `json:"at"`
	Bytes activityBytes `json:"bytes"`
}

// Activity retains bounded counter differences, never raw flows.
type Activity struct {
	mu       sync.Mutex
	previous map[string]Usage
	last     time.Time
	buckets  map[int64]map[netip.Addr]activityBytes
	entries  int
	path     string
}

func NewActivity() *Activity { return &Activity{buckets: make(map[int64]map[netip.Addr]activityBytes)} }

// OpenActivity restores history, but never restores counter baselines across restarts.
func OpenActivity(path string) (*Activity, error) {
	a := NewActivity()
	a.path = path
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return a, nil
	}
	if err != nil {
		return nil, err
	}
	var stored struct {
		Version int                                    `json:"version"`
		Buckets map[int64]map[netip.Addr]activityBytes `json:"buckets"`
	}
	if err := json.Unmarshal(data, &stored); err != nil {
		return nil, fmt.Errorf("reading activity history: %w", err)
	}
	if stored.Version != 1 {
		return nil, fmt.Errorf("unsupported activity history version %d", stored.Version)
	}
	now := time.Now()
	for at, bucket := range stored.Buckets {
		if at < now.Add(-14*24*time.Hour).Unix() || at > now.Unix() {
			continue
		}
		for addr := range bucket {
			if !addr.IsValid() {
				return nil, fmt.Errorf("invalid activity address")
			}
		}
		a.entries += len(bucket)
		if a.entries > 100000 {
			return nil, fmt.Errorf("activity history exceeds entry limit")
		}
		a.buckets[at] = bucket
	}
	return a, nil
}

// Save checkpoints history atomically. Counter baselines are deliberately omitted.
func (a *Activity) Save() error {
	if a.path == "" {
		return nil
	}
	a.mu.Lock()
	data, err := json.Marshal(struct {
		Version int                                    `json:"version"`
		Buckets map[int64]map[netip.Addr]activityBytes `json:"buckets"`
	}{1, a.buckets})
	a.mu.Unlock()
	if err != nil {
		return err
	}
	return core.WriteFileAtomic(a.path, data, 0o600)
}

func (a *Activity) observe(now time.Time, usage []Usage, valid bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for at, bucket := range a.buckets {
		if at < now.Add(-14*24*time.Hour).Unix() {
			a.entries -= len(bucket)
			delete(a.buckets, at)
		}
	}
	if !valid {
		a.previous = nil
		a.last = time.Time{}
		return
	}
	next := make(map[string]Usage, len(usage))
	at := now.Truncate(30 * time.Minute).Unix()
	continuous := !a.last.IsZero() && now.After(a.last) && now.Sub(a.last) <= 2*time.Minute && a.last.Truncate(30*time.Minute).Unix() == at
	for _, u := range usage {
		key := u.Addr.String() + "\x00" + u.Exit
		next[key] = u
		old, exists := a.previous[key]
		if !continuous || !exists || u.UpBytes < old.UpBytes || u.DownBytes < old.DownBytes {
			continue
		}
		bucket := a.buckets[at]
		if bucket == nil {
			bucket = make(map[netip.Addr]activityBytes)
			a.buckets[at] = bucket
		}
		value, held := bucket[u.Addr]
		if !held {
			if a.entries >= 100000 {
				continue
			}
			a.entries++
		}
		value.Up += u.UpBytes - old.UpBytes
		value.Down += u.DownBytes - old.DownBytes
		bucket[u.Addr] = value
	}
	a.previous, a.last = next, now
}

func (a *Activity) Run(ctx context.Context, applier Applier) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	checkpoint := time.NewTicker(time.Minute)
	defer checkpoint.Stop()
	for {
		cfg, err := applier.Load()
		var usage []Usage
		valid := err == nil && cfg.StatsOrDefault()
		if valid {
			usage, err = applier.Traffic(ctx)
			valid = err == nil
		}
		a.observe(time.Now(), usage, valid)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-checkpoint.C:
			if err := a.Save(); err != nil {
				slog.Error("could not save activity history", "error", err)
			}
		}
	}
}

func (a *Activity) snapshot(addresses []netip.Addr) []activityBucket {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]activityBucket, 0)
	for at, values := range a.buckets {
		var total activityBytes
		found := false
		for _, addr := range addresses {
			if v, ok := values[addr]; ok {
				found = true
				total.Up += v.Up
				total.Down += v.Down
			}
		}
		if found {
			out = append(out, activityBucket{At: time.Unix(at, 0), Bytes: total})
		}
	}
	return out
}
