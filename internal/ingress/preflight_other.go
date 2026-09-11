//go:build !linux

package ingress

// PortConflict cannot be answered without procfs. Reporting "no conflict" is
// safe here because this build has no systemd either, so nothing will be started
// for the check to have protected.
//
// Only the detection is stubbed. ErrPortInUse is shared (preflight.go), so the
// refusal an operator would read is identical on every platform and testable on
// any of them.
func PortConflict() ([]uint64, error) { return nil, nil }
