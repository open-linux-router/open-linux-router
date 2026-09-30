package dhcpv6

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"syscall"
	"time"
)

// UDPConn is Conn over a UDP socket bound to one interface's link-local
// address, port 546 — where RFC 8415 §7.2 has clients listen, and the source
// address a server replies to.
//
// Bound to the link-local address rather than the wildcard, so the socket is
// the interface's and no other: a reply on the LAN side for somebody else's
// client never reaches it, and the multicast Solicit leaves by the right
// interface because the destination carries the same zone.
type UDPConn struct {
	conn  *net.UDPConn
	iface string
}

// ErrInUse is Listen's answer when something already holds the client port on
// the interface — in practice the distribution's own DHCP client.
var ErrInUse = errors.New("another DHCPv6 client is already running on this interface")

// ErrNoLinkLocal is Listen's answer when the interface has no usable
// link-local address yet: it is down, or still doing duplicate address
// detection a moment after coming up.
var ErrNoLinkLocal = errors.New("the interface has no IPv6 link-local address yet")

// Listen opens the client socket on iface.
func Listen(iface string) (*UDPConn, error) {
	ll, err := linkLocal(iface)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp6", &net.UDPAddr{IP: ll.AsSlice(), Port: ClientPort, Zone: iface})
	switch {
	case errors.Is(err, syscall.EADDRINUSE):
		return nil, fmt.Errorf("%s: %w", iface, ErrInUse)
	case errors.Is(err, syscall.EADDRNOTAVAIL):
		// Still tentative: DAD has not finished, and the kernel will not bind
		// an address it may yet have to give up.
		return nil, fmt.Errorf("%s: %w", iface, ErrNoLinkLocal)
	case err != nil:
		return nil, err
	}
	return &UDPConn{conn: conn, iface: iface}, nil
}

// linkLocal finds iface's fe80:: address.
func linkLocal(iface string) (netip.Addr, error) {
	ifi, err := net.InterfaceByName(iface)
	if err != nil {
		return netip.Addr{}, err
	}
	addrs, err := ifi.Addrs()
	if err != nil {
		return netip.Addr{}, err
	}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		addr, ok := netip.AddrFromSlice(ipnet.IP)
		if ok && addr.Is6() && !addr.Is4In6() && addr.IsLinkLocalUnicast() {
			return addr, nil
		}
	}
	return netip.Addr{}, fmt.Errorf("%s: %w", iface, ErrNoLinkLocal)
}

// Send implements Conn.
func (c *UDPConn) Send(_ context.Context, b []byte) error {
	_, err := c.conn.WriteToUDP(b, &net.UDPAddr{IP: AllServers.AsSlice(), Port: ServerPort, Zone: c.iface})
	return err
}

// Receive implements Conn. The read deadline is whichever comes first, the
// caller's or ctx's, so a cancelled client stops within one read.
func (c *UDPConn) Receive(ctx context.Context, deadline time.Time) ([]byte, error) {
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := c.conn.SetReadDeadline(deadline); err != nil {
		return nil, err
	}
	buf := make([]byte, 1500)
	for {
		n, _, err := c.conn.ReadFromUDP(buf)
		if err != nil {
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				return nil, ErrTimeout
			}
			return nil, err
		}
		return append([]byte(nil), buf[:n]...), nil
	}
}

// Close closes the socket.
func (c *UDPConn) Close() error { return c.conn.Close() }
