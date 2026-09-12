//go:build linux

package core

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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

// UDPPortHolder and TCPPortHolder name the process holding a port.
//
// The Holder type and its rendering live in portholder.go, outside this file's
// build tag: only the lookup needs procfs.
//
// Best-effort, and deliberately errorless: every caller is building an error
// message about a conflict it has already detected, and a lookup that failed
// must degrade to a vaguer sentence rather than replace a real diagnosis with a
// complaint about procfs. Not-found and could-not-tell are the same answer here
// because the caller does the same thing with both.
//
// Finding the pid behind a socket means matching its inode against every
// process's open descriptors, which only root may read for other users'
// processes. That is olrd, which is where these messages are built.
func UDPPortHolder(port uint64) (Holder, bool) {
	return portHolder(procRoot, []string{"/proc/net/udp", "/proc/net/udp6"}, port)
}

func TCPPortHolder(port uint64) (Holder, bool) {
	return portHolder(procRoot, []string{"/proc/net/tcp", "/proc/net/tcp6"}, port)
}

// procRoot is a variable only so the tests can point the walk at a fixture.
var procRoot = "/proc"

// inodeField is the socket inode's column in /proc/net/{tcp,udp}. The columns
// before it are fixed across every kernel that has this file; new ones are
// appended after, which is why this counts from the left.
const inodeField = 9

func portHolder(root string, netFiles []string, port uint64) (Holder, bool) {
	inodes := map[string]bool{}
	for _, path := range netFiles {
		// A read error is one more way of not knowing who holds the port.
		_ = eachPortRow(path, func(fields []string, listening uint64) {
			if listening == port && len(fields) > inodeField {
				inodes[fields[inodeField]] = true
			}
		})
	}
	if len(inodes) == 0 {
		return Holder{}, false
	}
	return holderOf(root, inodes)
}

// holderOf finds the first process with one of these socket inodes open.
//
// First, not all: two processes sharing a listening socket (a forking daemon,
// or systemd socket activation) are one answer to the operator's question, and
// listing both would suggest a complication that is not there.
func holderOf(root string, inodes map[string]bool) (Holder, bool) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return Holder{}, false
	}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue // /proc has plenty that is not a process
		}
		fdDir := filepath.Join(root, e.Name(), "fd")
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			// Not ours to read, or the process exited mid-walk. Both are
			// ordinary; neither is worth abandoning the search for.
			continue
		}
		for _, fd := range fds {
			target, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
			if err != nil {
				continue
			}
			inode, ok := strings.CutPrefix(target, "socket:[")
			if !ok {
				continue
			}
			inode, ok = strings.CutSuffix(inode, "]")
			if !ok || !inodes[inode] {
				continue
			}
			return Holder{
				PID:  pid,
				Name: procField(root, e.Name(), "comm"),
				Unit: unitOf(procField(root, e.Name(), "cgroup")),
			}, true
		}
	}
	return Holder{}, false
}

func procField(root, pid, name string) string {
	data, err := os.ReadFile(filepath.Join(root, pid, name))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
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

// ListeningTCPPorts reports every port this box is accepting TCP connections on.
//
// The companion to TCPPortInUse above, and it answers a deliberately narrower
// question. That one asks "is anything at all using this port", which is the
// right test before *binding* it — an established connection on the port would
// make a bind fail too. This one asks "is this box serving here", which is the
// right test before *forwarding* it onward: internal/firewall needs to warn that
// a forward would take a port away from a local service, and an outbound
// connection that happens to have been given that number as its ephemeral source
// port is not a local service.
//
// The difference is not academic. A forward of 30000-40000 for a game overlaps
// the ephemeral range, so without the state filter every outbound connection on
// the box would read as a conflict and the warning would be worthless.
func ListeningTCPPorts() ([]uint16, error) {
	// 0x0A is TCP_LISTEN. The rest of the state machine is connections.
	return portsListedIn([]string{"/proc/net/tcp", "/proc/net/tcp6"},
		func(fields []string) bool { return len(fields) > 3 && fields[3] == "0A" })
}

// ListeningUDPPorts reports every port this box has an unconnected UDP socket
// bound to.
//
// UDP has no listen state, so the filter is the remote address instead: a socket
// with no peer is one waiting to be spoken to, and a socket with one is this box
// talking to somebody else.
func ListeningUDPPorts() ([]uint16, error) {
	return portsListedIn([]string{"/proc/net/udp", "/proc/net/udp6"},
		func(fields []string) bool {
			if len(fields) < 3 {
				return false
			}
			_, remPort, ok := strings.Cut(fields[2], ":")
			return ok && strings.Trim(remPort, "0") == ""
		})
}

// portsListedIn collects the local ports of every row a filter keeps.
//
// Sorted and deduplicated, because the same port appears once per address family
// and once per socket, and a caller showing this to an operator wants the port
// named once.
func portsListedIn(paths []string, keep func(fields []string) bool) ([]uint16, error) {
	seen := map[uint16]bool{}
	for _, path := range paths {
		if err := eachPortRow(path, func(fields []string, port uint64) {
			if keep(fields) {
				seen[uint16(port)] = true
			}
		}); err != nil {
			return nil, err
		}
	}

	out := make([]uint16, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

func portListedIn(path string, port uint64) (bool, error) {
	found := false
	err := eachPortRow(path, func(_ []string, listening uint64) {
		if listening == port {
			found = true
		}
	})
	return found, err
}

// eachPortRow parses one procfs socket table and hands each row's fields and
// local port to fn.
//
// The two readers above differ only in what they do with a row, so the format —
// a header line, then rows whose second field is "HEXADDR:HEXPORT" — is decoded
// in exactly one place. A malformed row is skipped rather than failing the read:
// procfs grows columns between kernel versions, and a new one at the end must
// not stop olr from answering.
func eachPortRow(path string, fn func(fields []string, port uint64)) error {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		// No procfs entry to read: report "cannot tell" as "no conflict"
		// rather than blocking a legitimate start on a missing file.
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
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
		port, err := strconv.ParseUint(hexPort, 16, 32)
		if err != nil {
			continue
		}
		fn(fields, port)
	}
	return scanner.Err()
}
