package dhcp

import (
	"encoding/binary"
	"fmt"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"
)

// Leases are an observed resource, not configuration (design.md §6.2): read
// from the daemon's own database, never stored by us and never revisioned. They
// are also the fact this module publishes one-way to dns and clients
// (design.md §4.1), so the parser is the interface those modules will consume.

// Lease is one entry in dnsmasq's lease database.
type Lease struct {
	// Expires is when the lease runs out. Zero means it never does — dnsmasq
	// writes an expiry of 0 for an infinite lease.
	Expires time.Time

	// MAC is the client's hardware address for a DHCPv4 lease. Empty for
	// DHCPv6, where the hardware address is frequently not available at all.
	MAC string

	// IAID identifies the DHCPv6 identity association. Empty for DHCPv4.
	IAID string

	// IP is the leased address.
	IP netip.Addr

	// Hostname is what the client called itself, or empty if it said nothing.
	Hostname string

	// ClientID is the DHCPv4 client identifier or the DHCPv6 DUID, or empty.
	ClientID string
}

// Active reports whether the lease is still valid at now.
func (l Lease) Active(now time.Time) bool {
	return l.Expires.IsZero() || l.Expires.After(now)
}

// ParseLeases reads a dnsmasq lease database.
//
// Malformed lines are reported rather than returned as a fatal error. A lease
// file is written by a running daemon and may legitimately be caught
// mid-write, and one bad line should not turn `olr dhcp show leases` into an
// error page listing nothing. Reporting them keeps that honest — silently
// dropping lines would make the count wrong with no way to tell (§3.4).
func ParseLeases(data []byte) ([]Lease, []Problem) {
	var (
		leases   []Lease
		problems []Problem
	)

	for n, line := range strings.Split(string(data), "\n") {
		text := strings.TrimSpace(line)
		if text == "" {
			continue
		}
		// dnsmasq writes the server's own DUID as a "duid <value>" line when
		// DHCPv6 is in use. It is not a lease.
		if strings.HasPrefix(text, "duid ") {
			continue
		}

		lease, err := parseLease(text)
		if err != nil {
			problems = append(problems, Problem{
				Path:    fmt.Sprintf("line %d", n+1),
				Message: err.Error(),
			})
			continue
		}
		leases = append(leases, lease)
	}

	return leases, problems
}

// parseLease reads one line: expiry, identity, address, hostname, client id.
func parseLease(text string) (Lease, error) {
	fields := strings.Fields(text)
	if len(fields) < 3 {
		return Lease{}, fmt.Errorf("expected at least 3 fields, got %d", len(fields))
	}

	epoch, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return Lease{}, fmt.Errorf("expiry %q is not a timestamp", fields[0])
	}
	ip, err := netip.ParseAddr(fields[2])
	if err != nil {
		return Lease{}, fmt.Errorf("%q is not an IP address", fields[2])
	}

	lease := Lease{IP: ip}
	if epoch != 0 {
		lease.Expires = time.Unix(epoch, 0).UTC()
	}

	// The second field is a hardware address for IPv4 and a numeric IAID for
	// IPv6. Which one it is follows from the address family, not from guessing
	// at the text.
	if ip.Is4() {
		mac, err := NormalizeMAC(fields[1])
		if err != nil {
			return Lease{}, fmt.Errorf("%q is not a MAC address", fields[1])
		}
		lease.MAC = mac
	} else {
		lease.IAID = fields[1]
	}

	// dnsmasq writes "*" for a field the client did not supply.
	if len(fields) > 3 && fields[3] != "*" {
		lease.Hostname = fields[3]
	}
	if len(fields) > 4 && fields[4] != "*" {
		lease.ClientID = fields[4]
	}

	return lease, nil
}

// LoadLeases reads the lease database. A missing file means the daemon has
// never handed out a lease, which is not an error.
func LoadLeases(path string) ([]Lease, []Problem, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("reading %s: %w", path, err)
	}
	leases, problems := ParseLeases(data)
	return leases, problems, nil
}

// Usage summarises how full a pool is.
type Usage struct {
	// Interface is the pool this describes.
	Interface string
	// Size is the number of addresses in the range.
	Size int
	// Active is how many currently hold a live lease.
	Active int
	// Expired is how many hold a lease that has run out but not been reclaimed.
	Expired int
}

// Free is the number of addresses still available.
func (u Usage) Free() int { return u.Size - u.Active }

// Percent is pool utilisation, 0-100.
func (u Usage) Percent() int {
	if u.Size == 0 {
		return 0
	}
	return u.Active * 100 / u.Size
}

// UsageOf counts leases falling inside a pool's range.
func UsageOf(p Pool, leases []Lease, now time.Time) Usage {
	u := Usage{Interface: p.Interface, Size: RangeSize(p.Start, p.End)}
	for _, l := range leases {
		if !inRange(p.Start, p.End, l.IP) {
			continue
		}
		if l.Active(now) {
			u.Active++
		} else {
			u.Expired++
		}
	}
	return u
}

// LeasesIn returns the leases inside a pool's range.
//
// This is also what makes the "disruptive" impact classification real rather
// than a guess: a range change is disruptive exactly when a client currently
// holds an address the new range no longer covers.
func LeasesIn(start, end netip.Addr, leases []Lease) []Lease {
	var out []Lease
	for _, l := range leases {
		if inRange(start, end, l.IP) {
			out = append(out, l)
		}
	}
	return out
}

// RangeSize counts the addresses in an inclusive IPv4 range.
func RangeSize(start, end netip.Addr) int {
	if !start.Is4() || !end.Is4() || start.Compare(end) > 0 {
		return 0
	}
	s, e := start.As4(), end.As4()
	return int(binary.BigEndian.Uint32(e[:]) - binary.BigEndian.Uint32(s[:]) + 1)
}
