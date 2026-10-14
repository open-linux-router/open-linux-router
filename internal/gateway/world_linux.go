package gateway

import (
	"context"
	"fmt"
	"net/netip"
	"os"
	"strings"

	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

func worldPublic(ip netip.Addr) bool {
	return ip.IsValid() && ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !core.IsCGNAT(ip)
}

func readWorldFlows(ctx context.Context) ([]worldFlow, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	b, err := os.ReadFile("/proc/sys/net/netfilter/nf_conntrack_acct")
	if err != nil {
		return nil, false, err
	}
	if strings.TrimSpace(string(b)) != "1" {
		return nil, false, nil
	}
	addresses, err := netlink.AddrList(nil, netlink.FAMILY_ALL)
	if err != nil {
		return nil, false, err
	}
	own := map[netip.Addr]bool{}
	for _, a := range addresses {
		if ip, ok := netip.AddrFromSlice(a.IP); ok {
			own[ip.Unmap()] = true
		}
	}
	var out []worldFlow
	for _, family := range []netlink.InetFamily{unix.AF_INET, unix.AF_INET6} {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		flows, err := netlink.ConntrackTableList(netlink.ConntrackTable, family)
		if err != nil {
			return nil, false, err
		}
		for _, f := range flows {
			src, ok := netip.AddrFromSlice(f.Forward.SrcIP)
			if !ok {
				continue
			}
			src = src.Unmap()
			dst, ok := netip.AddrFromSlice(f.Forward.DstIP)
			if !ok {
				continue
			}
			dst = dst.Unmap()
			// Exactly one original endpoint must be local. This includes
			// inbound flows and router-originated traffic, but excludes LAN↔LAN.
			srcLocal, dstLocal := own[src] || !worldPublic(src), own[dst] || !worldPublic(dst)
			if srcLocal == dstLocal {
				continue
			}
			ip, up, down := dst, f.Forward.Bytes, f.Reverse.Bytes
			if dstLocal {
				ip, up, down = src, f.Reverse.Bytes, f.Forward.Bytes
			}
			if !worldPublic(ip) {
				continue
			}
			key := fmt.Sprintf("%d/%d/%s/%d/%s/%d/%d", f.Zone, f.Forward.Protocol, src, f.Forward.SrcPort, dst, f.Forward.DstPort, f.TimeStart)
			out = append(out, worldFlow{Key: key, IP: ip, Up: up, Down: down})
		}
	}
	return out, true, nil
}
