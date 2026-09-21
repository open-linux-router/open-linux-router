package daemon

import (
	"context"
	"log/slog"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/dhcp"
	"github.com/open-linux-router/open-linux-router/internal/dns"
)

// The background watch: intent says a backend answers this network, and it is
// not running.
//
// systemd already owns "keep it running" — every backend unit is Restart=always
// — so this is not a second supervisor and must not behave like one. What
// systemd cannot know is the case it was never told about: a unit that was
// enabled after the last boot and therefore never started, or one somebody
// stopped by hand. `systemctl enable` does not start anything, and a stopped
// unit is a stopped unit as far as systemd is concerned; it is olr that holds
// the opinion that this box should be answering DNS.
//
// Startup convergence (startDNS) already covers the first of those. This covers
// the hours in between, where the only alternative was an operator noticing.

// superviseEvery is how often the watch looks.
//
// Slow on purpose. This is not a health check and nothing about it is urgent:
// the crash that this would catch quickly is the one systemd already restarts
// in two seconds, and what is left — a unit nobody started — does not get worse
// for being found a minute later. A fast loop would only buy a tighter window
// on a rare event and pay for it with a drift read every few seconds forever.
const superviseEvery = time.Minute

// giveUpAfter is how many consecutive attempts the watch makes before leaving a
// backend alone.
//
// There has to be a limit, and the reason is that the failures this cannot fix
// look identical to the ones it can. A resolver that will not parse its config,
// a port something else holds — no number of restarts helps, and a watch that
// kept trying would turn a diagnosable fault into a log nobody can read, while
// fighting systemd's own restart counter for the same unit.
//
// Three, and then silence. The box stays drifted, which is not a state olr
// hides: the section's page already says the backend is not running and offers
// the button that runs the same apply with a human watching — and that path
// reports which step failed and why, which is what somebody actually needs by
// then.
const giveUpAfter = 3

// backend is one module's service half, as the watch sees it.
//
// Two functions rather than one, because "is anything wrong" and "do something
// about it" are asked at different times: the watch keeps asking the first
// after it has stopped doing the second, which is how a backend that somebody
// fixed by hand comes back under supervision without olrd being restarted.
type backend struct {
	name string

	// pending reports that intent says this backend should be running, it is
	// not, and starting it is the whole of the work.
	pending func(context.Context) (bool, error)

	// start does that work.
	start func(context.Context) error
}

// superviseBackends watches the modules that drive a daemon and starts one that
// intent says should be running.
//
// Returns when ctx is done, so it dies with olrd rather than outliving it.
func superviseBackends(ctx context.Context, backends []backend, every time.Duration, logger *slog.Logger) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()

	// Attempts since the backend was last seen healthy, per module. Reset by
	// finding nothing to do rather than by a start that returned no error: a
	// daemon that accepts the start job and dies a minute later would otherwise
	// be restarted forever at one minute per cycle, which is the crash loop
	// this is supposed to stay out of.
	attempts := map[string]int{}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		for _, b := range backends {
			pending, err := b.pending(ctx)
			if err != nil {
				// Reading the box failed, which is not the same as the box
				// being wrong. Nothing is attempted and nothing is counted
				// against the backend — a system bus that was briefly
				// unavailable must not use up an attempt.
				logger.Debug("could not read whether a backend is running",
					"module", b.name, "error", err)
				continue
			}

			if !pending {
				if attempts[b.name] > 0 {
					logger.Info("the backend is running again; olr is watching it once more",
						"module", b.name)
				}
				delete(attempts, b.name)
				continue
			}

			if attempts[b.name] >= giveUpAfter {
				continue
			}
			attempts[b.name]++

			if err := b.start(ctx); err != nil {
				// Said at Error and named as what it is, because by the third
				// one olr has stopped trying and the only thing that will fix
				// this box is somebody reading this line.
				logger.Error("could not start a backend its settings say should be running",
					"module", b.name, "attempt", attempts[b.name], "of", giveUpAfter, "error", err)
				if attempts[b.name] >= giveUpAfter {
					logger.Error("giving up on this backend until it is seen running again; "+
						"the section's page shows what is wrong and can run the same apply",
						"module", b.name)
				}
				continue
			}

			logger.Info("a backend its settings say should be running was not, and has been started",
				"module", b.name)
		}
	}
}

// dnsBackend is the watch's view of the resolver.
func dnsBackend(a dns.Applier) backend {
	return backend{
		name: dns.ModuleName,
		pending: func(ctx context.Context) (bool, error) {
			cfg, err := a.Load()
			if err != nil {
				return false, err
			}
			if !cfg.Enabled {
				return false, nil
			}
			plan, err := a.Drift(ctx)
			if err != nil {
				return false, err
			}
			return startIsTheWholeJob(plan.StartsABackend(), plan.RewritesFiles(),
				plan.Impact >= dns.ImpactDisruptive), nil
		},
		start: func(ctx context.Context) error {
			cfg, err := a.Load()
			if err != nil {
				return err
			}
			_, err = a.Apply(ctx, cfg)
			return err
		},
	}
}

// dhcpBackend is the watch's view of the address server.
func dhcpBackend(a dhcp.Applier) backend {
	return backend{
		name: dhcp.ModuleName,
		pending: func(ctx context.Context) (bool, error) {
			cfg, err := a.Load()
			if err != nil {
				return false, err
			}
			if !cfg.Enabled {
				return false, nil
			}
			plan, err := a.Drift(ctx)
			if err != nil {
				return false, err
			}
			return startIsTheWholeJob(plan.StartsABackend(), plan.RewritesFiles(),
				plan.Impact >= dhcp.ImpactDisruptive), nil
		},
		start: func(ctx context.Context) error {
			cfg, err := a.Load()
			if err != nil {
				return err
			}
			_, err = a.Apply(ctx, cfg)
			return err
		},
	}
}

// startIsTheWholeJob decides whether the watch may act, given what a module's
// plan found. Written once because both modules have to answer it identically
// and the reasoning is the entire policy of this file.
func startIsTheWholeJob(starts, rewritesFiles, disruptive bool) bool {
	switch {
	case !starts:
		// Nothing is down. Whatever else the plan holds is somebody's pending
		// edit, not an outage, and none of it is this watch's business.
		return false

	case rewritesFiles:
		// The backend is down *and* a rendered file on disk is not the one
		// stored intent produces. Applying here would start the daemon and
		// silently revert that file in the same breath — and olr cannot tell a
		// hand-edit somebody is in the middle of from a file left behind by a
		// half-finished apply. Reverting the first without being asked is the
		// behaviour that makes configuration management hated, so the whole
		// case goes to the surface that has a person in front of it: the
		// section's page shows the diff and the button.
		return false

	case disruptive:
		// Putting this back would cost somebody an answer they are currently
		// getting. design.md §5.3.3 says the operator hears about those before
		// they happen, and there is nobody here to tell.
		return false

	default:
		return true
	}
}
