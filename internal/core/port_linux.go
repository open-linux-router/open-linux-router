//go:build linux

package core

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Who already holds a port.
//
// Every module that starts a backend on a well-known port needs this, and for
// the same reason: olr runs its own instance rather than taking over the
// distro's (design.md §3.4), so an operator can end up with two servers racing
// for one socket. Declaring Conflicts= in the unit would stop their daemon
// whenever ours starts — machine-wide interference of exactly the kind §3.4
// forbids — so instead we look first and refuse with an explanation.
//
// It reads procfs rather than trying to bind. A test bind proves nothing when
// the incumbent set SO_REUSEADDR, which dnsmasq does and most resolvers do.

// UDPPortInUse reports whether anything is listening on the given UDP port, on
// either address family.
func UDPPortInUse(port uint64) (bool, error) {
	return anyPortListedIn([]string{"/proc/net/udp", "/proc/net/udp6"}, port)
}

// TCPPortInUse reports whether anything is listening on the given TCP port, on
// either address family.
func TCPPortInUse(port uint64) (bool, error) {
	return anyPortListedIn([]string{"/proc/net/tcp", "/proc/net/tcp6"}, port)
}

func anyPortListedIn(paths []string, port uint64) (bool, error) {
	for _, path := range paths {
		inUse, err := portListedIn(path, port)
		if err != nil {
			return false, err
		}
		if inUse {
			return true, nil
		}
	}
	return false, nil
}

func portListedIn(path string, port uint64) (bool, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		// No procfs entry to read: report "cannot tell" as "no conflict"
		// rather than blocking a legitimate start on a missing file.
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("reading %s: %w", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Scan() // header

	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		// local_address is "HEXADDR:HEXPORT".
		_, hexPort, ok := strings.Cut(fields[1], ":")
		if !ok {
			continue
		}
		listening, err := strconv.ParseUint(hexPort, 16, 32)
		if err != nil {
			continue
		}
		if listening == port {
			return true, nil
		}
	}
	return false, scanner.Err()
}
