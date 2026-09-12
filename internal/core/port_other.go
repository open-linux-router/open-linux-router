//go:build !linux

package core

// Port occupancy cannot be answered without procfs. Reporting "no conflict" is
// safe here because this build has no systemd either, so nothing will be
// started for the check to have protected.

// UDPPortInUse always reports no conflict off Linux.
func UDPPortInUse(uint64) (bool, error) { return false, nil }

// TCPPortInUse always reports no conflict off Linux.
func TCPPortInUse(uint64) (bool, error) { return false, nil }

// Finding a holder means matching a socket inode against every process's open
// descriptors, which is procfs and nothing else. The Holder type and its
// rendering are shared (portholder.go) so that a message built off Linux — in a
// test, on a laptop — reads exactly as it will on the router.

// UDPPortHolder never finds a holder off Linux.
func UDPPortHolder(uint64) (Holder, bool) { return Holder{}, false }

// TCPPortHolder never finds a holder off Linux.
func TCPPortHolder(uint64) (Holder, bool) { return Holder{}, false }

// ListeningTCPPorts reports nothing off Linux, for the same reason: there is no
// procfs to read, and the module that asks is warning about a port this build
// could not forward anyway.
func ListeningTCPPorts() ([]uint16, error) { return nil, nil }

// ListeningUDPPorts reports nothing off Linux.
func ListeningUDPPorts() ([]uint16, error) { return nil, nil }
