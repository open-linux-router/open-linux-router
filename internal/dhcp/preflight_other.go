//go:build !linux

package dhcp

import "fmt"

// PortConflict cannot be answered without procfs. Reporting "no conflict" is
// safe here because this build has no systemd either, so nothing will be
// started for the check to have protected.
func PortConflict() (bool, error) { return false, nil }

// ErrPortInUse keeps the API identical across platforms.
func ErrPortInUse() error { return fmt.Errorf("UDP/67 is already in use") }
