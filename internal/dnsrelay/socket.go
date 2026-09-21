package dnsrelay

import (
	"fmt"
	"net"
	"net/netip"

	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

// Binding, and answering from the address the query was sent to.
//
// # Why this file exists
//
// The relay binds the wildcard by default (internal/dns.Config.Listen has the
// reasoning: an address written down once is a copy of a fact the kernel owns,
// and it goes stale the moment the network changes under the box). A wildcard
// socket introduces one problem that a per-address socket does not have, and it
// is silent rather than loud.
//
// A socket bound to 0.0.0.0 does not know which of the box's addresses a
// datagram was sent to. When it replies, the kernel picks the source address by
// looking up a route to the client — and that is not always the address the
// client asked. A DNS client checks: a reply whose source differs from the
// destination it sent to is discarded as an off-path response (it is the same
// check that makes blind spoofing hard). The result is resolution that works on
// a simple box and fails on a router with two addresses on one segment, which
// is the worst shape a bug can have.
//
// So every datagram is read with its destination attached (IP_PKTINFO, via
// x/net's portable wrappers) and answered with that address set as the source.
// This is what unbound and dnsmasq do for the same reason.
//
// TCP needs none of this: a connection's local address is fixed when it is
// accepted, so the reply leaves from the address the client connected to
// without anybody asking.

// udpConn is a bound UDP socket that can answer from the address a query
// arrived at.
//
// The two implementations differ only in which of x/net's packet types they
// hold; the interface exists because IPv4 and IPv6 control messages are
// separate types with separate flags, and the serve loop should not care.
type udpConn interface {
	// read returns one datagram, who sent it, and what it takes to answer from
	// the address it was sent to.
	read(buf []byte) (n int, from netip.AddrPort, reply replyTo, err error)

	// write answers, from the address in reply when the platform supplied one.
	write(b []byte, to netip.AddrPort, reply replyTo) error

	// LocalAddr is what the socket bound, for logging.
	LocalAddr() net.Addr

	Close() error
}

// replyTo is the address a query was sent to, carried back to the answer.
//
// Zero means "the platform did not tell us", which is not an error:
// SetControlMessage fails on hosts that do not support it, and the fallback —
// letting the kernel choose a source address — is exactly the behaviour this
// relay had before. Degrading to it costs correctness only in the multi-address
// case that the control message exists to fix.
type replyTo struct {
	src     net.IP
	ifIndex int
}

// listenUDP binds one UDP socket and asks for destination addresses on it.
//
// The network is pinned to udp4 or udp6 rather than left as "udp" on purpose.
// Go resolves a wildcard "udp" listen to a dual-stack AF_INET6 socket, so
// binding 0.0.0.0:53 and [::]:53 as "udp" would be one socket asked for twice
// and the second bind would fail — and the addresses it did deliver would be
// v4-mapped, which every consumer downstream would then have to unmap.
func listenUDP(addr netip.AddrPort) (udpConn, error) {
	addr = unmapPort(addr)
	if addr.Addr().Is4() {
		conn, err := net.ListenUDP("udp4", net.UDPAddrFromAddrPort(addr))
		if err != nil {
			return nil, err
		}
		p := ipv4.NewPacketConn(conn)
		// Best-effort: an error here leaves cm nil on every read, and write
		// falls back to the kernel's own source selection.
		_ = p.SetControlMessage(ipv4.FlagDst|ipv4.FlagInterface, true)
		return &udp4Conn{conn: conn, p: p}, nil
	}

	conn, err := net.ListenUDP("udp6", net.UDPAddrFromAddrPort(addr))
	if err != nil {
		return nil, err
	}
	p := ipv6.NewPacketConn(conn)
	_ = p.SetControlMessage(ipv6.FlagDst|ipv6.FlagInterface, true)
	return &udp6Conn{conn: conn, p: p}, nil
}

// listenTCP binds one TCP listener, with the same family pinning as listenUDP
// and for the same reason.
func listenTCP(addr netip.AddrPort) (*net.TCPListener, error) {
	addr = unmapPort(addr)
	network := "tcp6"
	if addr.Addr().Is4() {
		network = "tcp4"
	}
	return net.ListenTCP(network, net.TCPAddrFromAddrPort(addr))
}

type udp4Conn struct {
	conn *net.UDPConn
	p    *ipv4.PacketConn
}

func (c *udp4Conn) read(buf []byte) (int, netip.AddrPort, replyTo, error) {
	n, cm, src, err := c.p.ReadFrom(buf)
	if err != nil {
		return 0, netip.AddrPort{}, replyTo{}, err
	}
	from, ok := addrPortOf(src)
	if !ok {
		return 0, netip.AddrPort{}, replyTo{}, fmt.Errorf("unexpected source address %v", src)
	}
	var reply replyTo
	if cm != nil {
		reply = replyTo{src: cm.Dst, ifIndex: cm.IfIndex}
	}
	return n, from, reply, nil
}

func (c *udp4Conn) write(b []byte, to netip.AddrPort, reply replyTo) error {
	var cm *ipv4.ControlMessage
	if reply.src != nil {
		cm = &ipv4.ControlMessage{Src: reply.src, IfIndex: reply.ifIndex}
	}
	_, err := c.p.WriteTo(b, cm, net.UDPAddrFromAddrPort(to))
	return err
}

func (c *udp4Conn) LocalAddr() net.Addr { return c.conn.LocalAddr() }
func (c *udp4Conn) Close() error        { return c.conn.Close() }

type udp6Conn struct {
	conn *net.UDPConn
	p    *ipv6.PacketConn
}

func (c *udp6Conn) read(buf []byte) (int, netip.AddrPort, replyTo, error) {
	n, cm, src, err := c.p.ReadFrom(buf)
	if err != nil {
		return 0, netip.AddrPort{}, replyTo{}, err
	}
	from, ok := addrPortOf(src)
	if !ok {
		return 0, netip.AddrPort{}, replyTo{}, fmt.Errorf("unexpected source address %v", src)
	}
	var reply replyTo
	if cm != nil {
		reply = replyTo{src: cm.Dst, ifIndex: cm.IfIndex}
	}
	return n, from, reply, nil
}

func (c *udp6Conn) write(b []byte, to netip.AddrPort, reply replyTo) error {
	var cm *ipv6.ControlMessage
	if reply.src != nil {
		cm = &ipv6.ControlMessage{Src: reply.src, IfIndex: reply.ifIndex}
	}
	_, err := c.p.WriteTo(b, cm, net.UDPAddrFromAddrPort(to))
	return err
}

func (c *udp6Conn) LocalAddr() net.Addr { return c.conn.LocalAddr() }
func (c *udp6Conn) Close() error        { return c.conn.Close() }

// addrPortOf narrows what x/net hands back to the one shape this package uses.
func addrPortOf(a net.Addr) (netip.AddrPort, bool) {
	u, ok := a.(*net.UDPAddr)
	if !ok {
		return netip.AddrPort{}, false
	}
	return unmapPort(u.AddrPort()), true
}

// unmapPort puts a v4-mapped address back in its native form, so that family
// tests read the way they are written.
func unmapPort(a netip.AddrPort) netip.AddrPort {
	return netip.AddrPortFrom(a.Addr().Unmap(), a.Port())
}

// bound is one address's pair of sockets, kept together so that a partial bind
// can be unwound without the caller tracking halves.
type bound struct {
	addr netip.AddrPort
	udp  udpConn
	tcp  *net.TCPListener
}

// bindAll binds every address or none.
//
// All-or-nothing, because these are addresses an operator named. "Some of the
// sockets came up" is a state nothing downstream can describe — the unit is
// either notified ready or it is not — and a relay answering on half the
// addresses it was told to is the kind of half-working that takes longest to
// diagnose. Run turns the failure into a wildcard retry, which is a decision
// about intent and belongs there, not here.
func bindAll(addrs []netip.AddrPort) ([]bound, error) {
	var out []bound
	unwind := func() {
		for _, b := range out {
			b.udp.Close()
			b.tcp.Close()
		}
	}

	for _, addr := range addrs {
		udp, err := listenUDP(addr)
		if err != nil {
			unwind()
			return nil, fmt.Errorf("listening on %s/udp: %w", addr, err)
		}
		tcp, err := listenTCP(addr)
		if err != nil {
			udp.Close()
			unwind()
			return nil, fmt.Errorf("listening on %s/tcp: %w", addr, err)
		}
		out = append(out, bound{addr: addr, udp: udp, tcp: tcp})
	}
	return out, nil
}

// bindAny binds as many of the addresses as the kernel will take, and fails
// only when none of them bind. It returns what came up and why the rest did
// not.
//
// This is the wildcard's path, and the difference from bindAll is a box with
// IPv6 disabled. WildcardListen asks for both families; on such a box the
// AF_INET6 socket fails with EAFNOSUPPORT, and all-or-nothing would turn "this
// router does not do IPv6" into "this router does not do DNS" — a far worse
// regression than the one this whole change set exists to fix, and one that
// would only show up on the machines least able to diagnose it.
//
// The asymmetry with bindAll is deliberate rather than untidy. An address an
// operator wrote down that cannot be bound is a fact worth refusing over; a
// family this kernel does not have is not something anybody asked for.
func bindAny(addrs []netip.AddrPort) ([]bound, []error) {
	var (
		out  []bound
		errs []error
	)
	for _, addr := range addrs {
		udp, err := listenUDP(addr)
		if err != nil {
			errs = append(errs, fmt.Errorf("listening on %s/udp: %w", addr, err))
			continue
		}
		tcp, err := listenTCP(addr)
		if err != nil {
			udp.Close()
			errs = append(errs, fmt.Errorf("listening on %s/tcp: %w", addr, err))
			continue
		}
		out = append(out, bound{addr: addr, udp: udp, tcp: tcp})
	}
	return out, errs
}
