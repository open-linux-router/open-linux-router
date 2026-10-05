package dnsrelay

import (
	"bufio"
	"net"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxIdentityEntries = 8192

// identityCache is refreshed at most once per second, never once per query.
// Conflicting observations are deliberately left unknown rather than routing
// a stranger's DNS to an exit that can return fake addresses.
// For a static address ARP is the fallback; a lease/ARP disagreement is
// uncertain until the neighbour table catches up.
type identityCache struct {
	mu          sync.Mutex
	until       time.Time
	byIP        map[netip.Addr]string
	unknown     map[netip.Addr]bool
	leases, arp string
}

func (c *identityCache) mac(ip netip.Addr) (string, bool) {
	if c.leases == "" && c.arp == "" {
		return "", false
	}
	if !ip.IsValid() {
		return "", false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Now().After(c.until) {
		c.byIP, c.unknown = collectIdentityState(c.leases, c.arp, time.Now())
		c.until = time.Now().Add(time.Second)
	}
	return c.byIP[ip.Unmap()], c.unknown[ip.Unmap()]
}

// collectIdentity is used by tests to inspect the effective, unambiguous map.
func collectIdentity(leases, arp string, now time.Time) map[netip.Addr]string {
	found, _ := collectIdentityState(leases, arp, now)
	return found
}

func collectIdentityState(leases, arp string, now time.Time) (map[netip.Addr]string, map[netip.Addr]bool) {
	found := map[netip.Addr]string{}
	ambiguous := map[netip.Addr]bool{}
	add := func(addr netip.Addr, mac string) {
		hw, err := net.ParseMAC(mac)
		if !addr.Is4() || err != nil || len(hw) != 6 {
			return
		}
		mac = strings.ToLower(hw.String())
		if len(found) >= maxIdentityEntries && found[addr] == "" {
			return
		}
		if old := found[addr]; old != "" && old != mac {
			ambiguous[addr] = true
		}
		found[addr] = mac
	}
	if data, err := os.ReadFile(leases); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			f := strings.Fields(line)
			if len(f) < 3 {
				continue
			}
			expiry, err := strconv.ParseInt(f[0], 10, 64)
			if err != nil || (expiry != 0 && expiry <= now.Unix()) {
				continue
			}
			if addr, err := netip.ParseAddr(f[2]); err == nil {
				add(addr, f[1])
			}
		}
	}
	if data, err := os.ReadFile(arp); err == nil {
		s := bufio.NewScanner(strings.NewReader(string(data)))
		for s.Scan() {
			f := strings.Fields(s.Text())
			if len(f) < 6 {
				continue
			}
			flags, err := strconv.ParseUint(strings.TrimPrefix(f[2], "0x"), 16, 32)
			if err != nil || flags&2 == 0 {
				continue
			}
			if addr, err := netip.ParseAddr(f[0]); err == nil {
				add(addr, f[3])
			}
		}
	}
	for addr := range ambiguous {
		delete(found, addr)
	}
	return found, ambiguous
}
