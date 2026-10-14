package gateway

import (
	"context"
	"net/netip"
	"sync"
	"time"
)

type activityBytes struct {
	Up   uint64 `json:"up"`
	Down uint64 `json:"down"`
}
type activityBucket struct {
	At    time.Time     `json:"at"`
	Bytes activityBytes `json:"bytes"`
}

// Activity retains bounded, volatile counter differences, never raw flows.
type Activity struct {
	mu       sync.Mutex
	previous map[string]Usage
	last     time.Time
	buckets  map[int64]map[netip.Addr]activityBytes
	entries  int
}

func NewActivity() *Activity { return &Activity{buckets: make(map[int64]map[netip.Addr]activityBytes)} }

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
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
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
