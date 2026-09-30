package devices

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// When each device was last heard from.
//
// Presence says whether a device is here now, and says nothing once it has
// gone: a device that left at breakfast and one that left last month are both
// "away", and a quiet device that is here is only "idle". The map wants the
// one answer that covers all three — "seen 2h ago" — and no source holds it.
// The lease file records an expiry, the neighbour table a state, and neither
// survives the device being gone long enough to matter.
//
// So this is the one piece of presence the module keeps, and it is kept apart
// from olr.json on purpose. Intent is what an operator said; this is what the
// box observed, and it lives where observations do, under /var/lib, in a file
// that can be deleted at any time and costs nothing but history.
//
// **Heard, not listed.** A sighting in the sense of Presence is not evidence:
// the kernel keeps a STALE neighbour entry for a device that left an hour ago,
// and /proc/net/arp reports it as complete. What counts here is the neighbour
// being REACHABLE — the kernel confirmed it answers within the last half
// minute — which is also what any traffic through the router keeps it. A
// device that only ever talks to its neighbours on the same network is not
// heard, the same blind spot the traffic counters have.
//
// **Sampled in the background.** The page is not open most of the time, and a
// timestamp that only moved while somebody was looking would be a record of
// the operator. Samples are cheap (one netlink dump) and taken every few
// seconds; the file is written every few minutes, and on the way down, because
// a router's disk is often flash and a write every sample would be wear for
// nothing.

// SeenPath is where olrd keeps the record, under an optional root.
func SeenPath(root string) string {
	return filepath.Join(root, "/var/lib/open-linux-router/devices/seen.json")
}

// HeardSource says which hardware addresses have just been heard from.
type HeardSource interface {
	Heard(ctx context.Context) ([]string, error)
}

// Seen is the record, in memory, with the file behind it.
type Seen struct {
	// Path is the file. Empty keeps the record in memory only.
	Path string

	// Source is what is sampled. Nil means nothing is ever heard, and the
	// record only holds what the file had.
	Source HeardSource

	Log *slog.Logger

	mu    sync.Mutex
	last  map[string]time.Time
	dirty bool
	// failing is whether the last sample failed, so a box whose kernel
	// refuses the read logs it once rather than every few seconds.
	failing bool
}

// Forget drops a device not heard from in this long. Most of what the record
// would otherwise grow by is phones' randomised addresses, each a device that
// existed for one visit; a real device away this long is one nobody needs a
// "seen" time for.
const Forget = 90 * 24 * time.Hour

// seenFile is the file's shape. Versioned because it outlives the release
// that wrote it.
type seenFile struct {
	Version int                  `json:"version"`
	Seen    map[string]time.Time `json:"seen"`
}

// NewSeen reads the record, when there is one. An unreadable file is logged
// and started over: it is history, and a router that refused to start for the
// want of it would be a bad trade.
func NewSeen(path string, source HeardSource, log *slog.Logger) *Seen {
	s := &Seen{Path: path, Source: source, Log: log, last: map[string]time.Time{}}
	if path == "" {
		return s
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s
	}
	var f seenFile
	if err == nil {
		err = json.Unmarshal(data, &f)
	}
	if err != nil {
		s.logger().Warn("starting the last-seen record over", "path", path, "error", err)
		return s
	}
	for mac, at := range f.Seen {
		if canon, err := core.NormalizeMAC(mac); err == nil {
			s.last[canon] = at
		}
	}
	return s
}

func (s *Seen) logger() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

// Record marks the addresses as heard at `at`. Unreadable ones are skipped:
// the source is the kernel, and one odd entry is not worth the rest.
func (s *Seen) Record(at time.Time, macs []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, mac := range macs {
		canon, err := core.NormalizeMAC(mac)
		if err != nil {
			continue
		}
		if at.After(s.last[canon]) {
			s.last[canon] = at
			s.dirty = true
		}
	}
}

// LastSeen is when the device was last heard from.
func (s *Seen) LastSeen(mac string) (time.Time, bool) {
	if s == nil {
		return time.Time{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	at, ok := s.last[mac]
	return at, ok
}

// Sample asks the source once and records what it heard.
func (s *Seen) Sample(ctx context.Context, now time.Time) {
	if s.Source == nil {
		return
	}
	macs, err := s.Source.Heard(ctx)
	s.mu.Lock()
	was := s.failing
	s.failing = err != nil
	s.mu.Unlock()
	if err != nil {
		if !was {
			s.logger().Warn("could not read the neighbour table; last-seen times will not move", "error", err)
		}
		return
	}
	s.Record(now, macs)
}

// Flush writes the record when it has changed, dropping what is past Forget.
func (s *Seen) Flush(now time.Time) error {
	s.mu.Lock()
	for mac, at := range s.last {
		if now.Sub(at) > Forget {
			delete(s.last, mac)
			s.dirty = true
		}
	}
	if s.Path == "" || !s.dirty {
		s.mu.Unlock()
		return nil
	}
	data, err := json.Marshal(seenFile{Version: 1, Seen: s.last})
	s.dirty = false
	s.mu.Unlock()
	if err != nil {
		return err
	}
	if err := core.WriteFileAtomic(s.Path, data, 0o644); err != nil {
		s.mu.Lock()
		s.dirty = true
		s.mu.Unlock()
		return fmt.Errorf("writing %s: %w", s.Path, err)
	}
	return nil
}

// Run samples every `every` and writes every `write` until ctx is done. The
// last write is the caller's: see Flush.
func (s *Seen) Run(ctx context.Context, every, write time.Duration) {
	sample := time.NewTicker(every)
	defer sample.Stop()
	flush := time.NewTicker(write)
	defer flush.Stop()
	s.Sample(ctx, time.Now())
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-sample.C:
			s.Sample(ctx, now)
		case now := <-flush.C:
			if err := s.Flush(now); err != nil {
				s.logger().Warn("could not save last-seen times", "error", err)
			}
		}
	}
}
