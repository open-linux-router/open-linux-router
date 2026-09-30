package daemon

import (
	"context"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/dial"
	"github.com/open-linux-router/open-linux-router/internal/link"
)

// The walk of design.md §4.1's arrow from dial's delegated prefix to the
// networks numbered out of it.
//
// dial reads link, so link cannot read dial; the prefix is published instead,
// and this is the subscriber. When it moves — delegated for the first time,
// renumbered by the ISP, expired, or given back — the delegated networks'
// router addresses follow it, the unreachable route for the prefix is swapped,
// and everything built from the networks re-applies: dns derives its allow list
// from the addresses, so a network on a new prefix is refused until it does.

// delegationFollower reconciles the box against dial.Delegation's prefix.
type delegationFollower struct {
	delegation *dial.Delegation
	link       link.Applier
	writer     dial.Writer
	srv        *core.Server
	dependents func(context.Context) []core.Step
	logger     *slog.Logger

	// path is where the last applied prefix is kept. It has to outlive olrd:
	// a prefix the ISP changed while olrd was down left addresses from the old
	// one on the networks, and this is the only record of which to take off.
	path string
}

func newDelegationFollower(root string) *delegationFollower {
	return &delegationFollower{path: filepath.Join(root, "/var/lib/open-linux-router/dial/delegated-prefix")}
}

// changed is dial.Delegation.Changed. It never takes the lock itself — see
// that field — and the goroutine reconciles against the prefix as it is when
// it gets the lock, so two announcements in quick succession land as one.
func (f *delegationFollower) changed(netip.Prefix, netip.Prefix) {
	go f.reconcile()
}

func (f *delegationFollower) reconcile() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var now netip.Prefix
	err := f.srv.ApplyLock().Do(ctx, func() error {
		was := f.load()
		now = f.delegation.Prefix()

		// Run even when was == now: after a reboot the prefix comes back the
		// same and the addresses do not, and FollowDelegation adds what is
		// missing and retires nothing.
		steps, err := f.link.FollowDelegation(ctx, was, now)
		for _, s := range steps {
			if s.Error != "" {
				f.logger.Error("could not move a network to the delegated prefix", "step", s.Description, "error", s.Error)
			} else {
				f.logger.Info("followed the delegated prefix", "step", s.Description)
			}
		}
		if err != nil {
			return err
		}
		if err := f.writer.Unreachable(ctx, was, now); err != nil {
			f.logger.Error("could not route the delegated prefix", "error", err)
		}
		if err := f.save(now); err != nil {
			f.logger.Error("could not record the delegated prefix", "path", f.path, "error", err)
		}
		f.dependents(ctx)
		return nil
	})
	if err != nil {
		f.logger.Error("could not follow the delegated prefix", "prefix", now, "error", err)
		return
	}
	f.srv.Events().Publish(core.Event{Type: core.EventApplied, Module: link.ModuleName})
	f.srv.Events().Publish(core.Event{Type: core.EventApplied, Module: dial.ModuleName})
}

func (f *delegationFollower) load() netip.Prefix {
	data, err := os.ReadFile(f.path)
	if err != nil {
		return netip.Prefix{}
	}
	p, err := netip.ParsePrefix(strings.TrimSpace(string(data)))
	if err != nil {
		return netip.Prefix{}
	}
	return p
}

func (f *delegationFollower) save(p netip.Prefix) error {
	if !p.IsValid() {
		err := os.Remove(f.path)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(filepath.Dir(f.path), 0o755); err != nil {
		return err
	}
	return core.WriteFileAtomic(f.path, []byte(p.String()+"\n"), 0o644)
}
