//go:build !linux && !darwin

package cli

// There is no flock here. This build exists only so the tree compiles on
// platforms olr does not target (design.md §10, test hardware), and a platform
// that cannot run olr cannot have two of them racing. Reporting the lock as
// contended would be a more convincing lie than reporting it as taken.
func tryLock(string) (Unlock, bool, error) { return func() {}, true, nil }
