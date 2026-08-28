package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Applying is a read-modify-write: load the stored config, change one field,
// render the result, apply it. Two of those interleaved lose one of the edits
// and neither reports an error — the operator is told the reservation was added
// and it silently was not. On a router that is the worst shape a bug can have,
// because the damage surfaces days later as one device that cannot get an
// address.
//
// design.md §3.6 puts a single global apply lock in core and is explicit that
// modules do not take their own. Core does not exist yet, so this stands in for
// it, scoped to the config file actually being written. When core owns the
// config store this becomes the global lock and the call site in each module
// disappears rather than changing — which is why it lives in the shared package
// and takes a path, instead of each module growing its own.
//
// Reads never take it (§3.6). There is nothing to serialise against a plan,
// which reads live system state rather than anything we hold.

// lockTimeout bounds the wait. The contention budget here is a human clicking a
// toggle, so anything slower than this is a wedged process rather than a queue,
// and saying so is more useful than hanging.
const lockTimeout = 5 * time.Second

// lockPollInterval is how often a contended lock is retried.
const lockPollInterval = 50 * time.Millisecond

// Unlock releases an apply lock. Always safe to call exactly once.
type Unlock func()

// Lock takes the apply lock guarding a module's config file, waiting briefly
// for a concurrent apply to finish.
//
// The lock is advisory and held by an open descriptor, so it is released by the
// kernel if the process dies. A crashed olr therefore cannot wedge the next
// one, and there is no stale lock file to clean up by hand.
func Lock(configPath string) (Unlock, error) {
	if configPath == "" {
		return func() {}, nil
	}

	path := configPath + ".lock"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("preparing the apply lock: %w", err)
	}

	deadline := time.Now().Add(lockTimeout)
	for {
		unlock, held, err := tryLock(path)
		if err != nil {
			return nil, fmt.Errorf("taking the apply lock %s: %w", path, err)
		}
		if held {
			return unlock, nil
		}
		if !time.Now().Before(deadline) {
			return nil, fmt.Errorf(
				"another olr command has been applying for more than %s; "+
					"waiting would risk losing one of the two changes (lock: %s)",
				lockTimeout, path)
		}
		time.Sleep(lockPollInterval)
	}
}
