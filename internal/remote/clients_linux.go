package remote

import (
	"context"
	"net/netip"
	"os"
	"strings"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

// NewConnTable returns the kernel's connection table.
func NewConnTable() ConnTable { return linuxConns{} }

type linuxConns struct{}

// tcpEstablished is TCP_CONNTRACK_ESTABLISHED.
const tcpEstablished = 3

// Flows dumps the connection table, keeping flows addressed to this box.
//
// "Addressed to this box" is the destination being one of its own addresses,
// which is what separates somebody connecting to the proxy from a device in
// the house connecting out to a port with the same number somewhere else —
// both are in the table, and only the first is a client.
func (linuxConns) Flows(ctx context.Context) ([]Flow, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	own := map[netip.Addr]bool{}
	addrs, err := netlink.AddrList(nil, netlink.FAMILY_ALL)
	if err != nil {
		return nil, false, err
	}
	for _, a := range addrs {
		if ip, ok := netip.AddrFromSlice(a.IP); ok {
			own[ip.Unmap()] = true
		}
	}

	var out []Flow
	for _, family := range []netlink.InetFamily{unix.AF_INET, unix.AF_INET6} {
		flows, err := netlink.ConntrackTableList(netlink.ConntrackTable, family)
		if err != nil {
			return nil, false, err
		}
		for _, f := range flows {
			dst, ok := netip.AddrFromSlice(f.Forward.DstIP)
			if !ok || !own[dst.Unmap()] {
				continue
			}
			src, ok := netip.AddrFromSlice(f.Forward.SrcIP)
			if !ok {
				continue
			}
			flow := Flow{Src: src.Unmap(), DstPort: f.Forward.DstPort, Bytes: f.Forward.Bytes, Established: true}
			switch f.Forward.Protocol {
			case unix.IPPROTO_TCP:
				flow.Proto = "tcp"
				if tcp, ok := f.ProtoInfo.(*netlink.ProtoInfoTCP); ok {
					flow.Established = tcp.State == tcpEstablished
				}
			case unix.IPPROTO_UDP:
				flow.Proto = "udp"
			default:
				continue
			}
			out = append(out, flow)
		}
	}
	return out, accounting(), nil
}

// accounting reports whether the kernel counts bytes per connection.
func accounting() bool {
	b, err := os.ReadFile("/proc/sys/net/netfilter/nf_conntrack_acct")
	return err == nil && strings.TrimSpace(string(b)) == "1"
}
