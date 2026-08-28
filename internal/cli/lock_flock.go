//go:build linux || darwin

package cli

import (
	"errors"
	"os"
	"syscall"
)

// tryLock takes a non-blocking exclusive flock, reporting contention as
// (nil, false, nil) rather than as an error — a lock somebody else holds is an
// expected outcome to retry, not a failure to report.
func tryLock(path string) (Unlock, bool, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, false, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return func() {
		// Closing the descriptor releases the lock on its own; unlocking first
		// makes that explicit rather than incidental.
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, true, nil
}
