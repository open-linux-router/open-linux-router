//go:build !linux

package dhcp

// PortConflict cannot be answered without procfs. Reporting "no conflict" is
// safe here because this build has no systemd either, so nothing will be
// started for the check to have protected.
//
// Only the detection is stubbed now. ErrPortInUse moved to preflight.go so that
// the refusal an operator reads is the same text on every platform — this file
// used to carry a one-line version of it, which meant the message that mattered
// was the one nobody tested.
func PortConflict() (bool, error) { return false, nil }
