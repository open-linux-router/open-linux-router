//go:build linux

package link

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"slices"

	"github.com/vishvananda/netlink"
)

// The kernel half, and the only file in this module that knows what netlink is.
//
// stdlib `net` still reads the interface list (kernel.go) because enumeration
// needs nothing more. Writing does: there is no way to add or remove an address
// through `net`, and shelling out to `ip` is forbidden by design.md §3.6. The
// dependency is already in §8's budget and `internal/gateway` already carries
// it, so this costs nothing new.

// NewWriter returns the writer for this platform.
func NewWriter() Writer { return linuxWriter{} }

type linuxWriter struct{}

// Apply brings each interface to the addressing its network calls for.
//
// Order matters within an interface and is the reverse of what looks natural:
// **add before remove**. Renumbering a LAN means removing 192.168.1.1/24 and
// adding 172.16.1.1/24, and doing it in that order leaves a window with no
// address at all — during which the kernel drops the connected route, and any
// session running over that interface (quite possibly the operator's) dies
// rather than merely being interrupted. Adding first means the interface always
// holds at least one address, and the old one goes away while the new one is
// already carrying traffic.
//
// It does not stop at the first failure. One interface's address being refused
// says nothing about the next one, and an operator looking at a half-applied
// change is better served by a complete list of what happened than by a report
// that stops at the first line.
func (linuxWriter) Apply(ctx context.Context, desired []Desired) ([]Step, error) {
	var steps []Step
	var failed int

	for _, d := range desired {
		if err := ctx.Err(); err != nil {
			return steps, err
		}

		link, err := netlink.LinkByName(d.Interface)
		if err != nil {
			steps = append(steps, Step{
				Description: fmt.Sprintf("find %s", d.Interface),
				Error:       err.Error(),
			})
			failed++
			continue
		}

		have, err := readV4(link)
		if err != nil {
			steps = append(steps, Step{
				Description: fmt.Sprintf("read addresses on %s", d.Interface),
				Error:       err.Error(),
			})
			failed++
			continue
		}

		for _, want := range d.Addrs {
			if slices.Contains(have, want) {
				continue
			}
			step := Step{Description: fmt.Sprintf("add %s to %s", want, d.Interface)}
			if err := netlink.AddrAdd(link, toNetlinkAddr(want)); err != nil {
				step.Error = err.Error()
				failed++
			} else {
				step.Done = true
			}
			steps = append(steps, step)
		}

		// The removal half, and the whole of what AddOnly suppresses: every v4
		// address on a member that the network does not call for comes off.
		for _, got := range have {
			if d.AddOnly || slices.Contains(d.Addrs, got) {
				continue
			}
			step := Step{Description: fmt.Sprintf("remove %s from %s", got, d.Interface)}
			if err := netlink.AddrDel(link, toNetlinkAddr(got)); err != nil {
				step.Error = err.Error()
				failed++
			} else {
				step.Done = true
			}
			steps = append(steps, step)
		}

		if d.Up && link.Attrs().Flags&net.FlagUp == 0 {
			step := Step{Description: fmt.Sprintf("bring %s up", d.Interface)}
			if err := netlink.LinkSetUp(link); err != nil {
				step.Error = err.Error()
				failed++
			} else {
				step.Done = true
			}
			steps = append(steps, step)
		}
	}

	if failed > 0 {
		return steps, fmt.Errorf("%d of %d address operations failed", failed, len(steps))
	}
	return steps, nil
}

// readV4 lists an interface's IPv4 addresses as prefixes.
//
// Link-local is excluded to match what kernel.go publishes, so that the plan an
// operator approved and the set this file compares against cannot disagree
// about whether 169.254.x.x counts.
func readV4(link netlink.Link) ([]netip.Prefix, error) {
	addrs, err := netlink.AddrList(link, netlink.FAMILY_V4)
	if err != nil {
		return nil, err
	}
	var out []netip.Prefix
	for _, a := range addrs {
		if a.IPNet == nil {
			continue
		}
		prefix, ok := toPrefix(a.IPNet)
		if !ok || prefix.Addr().IsLinkLocalUnicast() {
			continue
		}
		out = append(out, prefix)
	}
	return out, nil
}

// toNetlinkAddr converts a prefix into the shape netlink wants.
//
// The mask is built from the prefix length rather than copied from anywhere, so
// a /24 cannot arrive here as a /120 through the v4-in-v6 route that toPrefix
// exists to close.
func toNetlinkAddr(p netip.Prefix) *netlink.Addr {
	addr := p.Addr().As4()
	return &netlink.Addr{
		IPNet: &net.IPNet{
			IP:   net.IP(addr[:]),
			Mask: net.CIDRMask(p.Bits(), 32),
		},
	}
}
