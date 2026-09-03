//go:build !linux

package core

// Port occupancy cannot be answered without procfs. Reporting "no conflict" is
// safe here because this build has no systemd either, so nothing will be
// started for the check to have protected.

// UDPPortInUse always reports no conflict off Linux.
func UDPPortInUse(uint64) (bool, error) { return false, nil }

// TCPPortInUse always reports no conflict off Linux.
func TCPPortInUse(uint64) (bool, error) { return false, nil }
