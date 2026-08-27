//go:build linux

package dhcp

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// dhcpServerPort is the UDP port a DHCPv4 server listens on.
const dhcpServerPort = 67

// PortConflict reports whether something already holds the DHCP server port.
//
// We start our own dnsmasq instance rather than taking over the distro's
// (design.md §3.4), which means the operator can end up with two DHCP servers
// racing for the same port. Rather than declaring Conflicts= in the unit and
// stopping their daemon — machine-wide interference of exactly the kind §3.4
// forbids — we look first and refuse with an explanation.
//
// It reads /proc/net/udp rather than trying to bind, because dnsmasq sets
// SO_REUSEADDR: a successful test bind would prove nothing.
func PortConflict() (bool, error) {
	for _, path := range []string{"/proc/net/udp", "/proc/net/udp6"} {
		inUse, err := portListedIn(path, dhcpServerPort)
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

// ErrPortInUse explains a refused start.
func ErrPortInUse() error {
	return fmt.Errorf(
		"UDP/%d is already in use, so another DHCP server is running on this box.\n"+
			"olr runs its own dnsmasq instance and will not stop somebody else's daemon.\n"+
			"Find the holder with `ss -lunp sport = :%d` and stop it, or leave DHCP to it",
		dhcpServerPort, dhcpServerPort)
}
