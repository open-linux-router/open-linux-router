package dhcpv6

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/netip"
	"time"
)

// Conn is the socket, one interface's worth: messages go to AllServers on the
// interface it is bound to, and what comes back is whatever arrived on the
// client port.
//
// An interface so the exchanges below are testable without a network, which
// is also why nothing in this file knows what a socket is.
type Conn interface {
	Send(ctx context.Context, b []byte) error

	// Receive waits until deadline for one datagram. It returns
	// ErrTimeout when none arrives, which is an ordinary outcome here — it is
	// what makes the next retransmission happen.
	Receive(ctx context.Context, deadline time.Time) ([]byte, error)
}

// ErrTimeout is Receive's "nothing arrived".
var ErrTimeout = errors.New("dhcpv6: timed out")

// ErrNoServer is returned when an exchange runs out of retransmissions with no
// usable answer. The words are the operator's: on an uplink this nearly always
// means the ISP does not delegate prefixes, or not to this interface.
var ErrNoServer = errors.New("no DHCPv6 server answered with a prefix")

// Lease is a delegated prefix and what it takes to keep it.
type Lease struct {
	Prefix    netip.Prefix
	Preferred time.Duration
	Valid     time.Duration
	T1, T2    time.Duration

	// ServerID is who delegated it; a Renew goes back to that server.
	ServerID []byte

	// DNS are the resolvers the server offered, when it offered any.
	DNS []netip.Addr

	// Obtained is when the Reply arrived. Every lifetime counts from here.
	Obtained time.Time
}

// Expires is when the prefix stops being valid.
func (l Lease) Expires() time.Time { return l.Obtained.Add(l.Valid) }

// Client runs the exchanges for one identity association.
type Client struct {
	Conn Conn

	// ClientID is the DUID (DUIDLL) and IAID the identity association's
	// number. The pair is what the ISP keys the delegation on.
	ClientID []byte
	IAID     uint32

	// Hint is the prefix length to ask for — 56, 60, 64 — or zero to take
	// whatever the ISP gives.
	Hint int

	// Now is the clock. Nil means time.Now.
	Now func() time.Time
}

func (c *Client) now() time.Time {
	if c.Now == nil {
		return time.Now()
	}
	return c.Now()
}

// Retransmission parameters (RFC 8415 §7.6). SOL_MAX_RT is capped at two
// minutes rather than the RFC's hour: an operator who has just fixed the ISP
// side is waiting at the status page, and a Solicit a minute costs nobody
// anything.
type schedule struct {
	irt, mrt time.Duration
	mrc      int           // zero: unlimited
	mrd      time.Duration // zero: unlimited
}

var (
	solicitSchedule = schedule{irt: time.Second, mrt: 2 * time.Minute}
	requestSchedule = schedule{irt: time.Second, mrt: 30 * time.Second, mrc: 10}
	renewSchedule   = schedule{irt: 10 * time.Second, mrt: 10 * time.Minute}
	releaseSchedule = schedule{irt: time.Second, mrt: time.Second, mrc: 4}
)

// Acquire solicits a prefix and requests it: Solicit, Advertise, Request,
// Reply — or Solicit and Reply, when the server does Rapid Commit.
//
// It runs until it has a lease or ctx ends. There is no giving up: a router
// whose ISP is not answering yet should be the router that gets a prefix the
// moment it does.
func (c *Client) Acquire(ctx context.Context) (Lease, error) {
	hint := IAPD{IAID: c.IAID}
	if c.Hint > 0 {
		hint.Prefixes = []IAPrefix{{Prefix: netip.PrefixFrom(netip.IPv6Unspecified(), c.Hint)}}
	}
	sol := Message{Type: Solicit, ClientID: c.ClientID, RequestDNS: true, RapidCommit: true, IAPD: &hint}

	got, err := c.exchange(ctx, sol, solicitSchedule, func(m Message) bool {
		switch m.Type {
		case Advertise:
			return usable(m, c.IAID)
		case Reply:
			return m.RapidCommit && usable(m, c.IAID)
		}
		return false
	})
	if err != nil {
		return Lease{}, err
	}
	if got.Type == Reply {
		return c.lease(got), nil
	}

	req := Message{Type: Request, ClientID: c.ClientID, ServerID: got.ServerID, RequestDNS: true,
		IAPD: &IAPD{IAID: c.IAID, Prefixes: got.IAPD.Prefixes}}
	reply, err := c.exchange(ctx, req, requestSchedule, func(m Message) bool {
		return m.Type == Reply
	})
	if err != nil {
		return Lease{}, err
	}
	if !usable(reply, c.IAID) {
		return Lease{}, refusal(reply)
	}
	return c.lease(reply), nil
}

// Renew extends a lease with the server that granted it, until T2. Rebind is
// the same without a server named, to anybody, until the lease expires.
func (c *Client) Renew(ctx context.Context, l Lease) (Lease, error) {
	return c.extend(ctx, l, Renew, l.ServerID, l.Obtained.Add(l.T2))
}

// Rebind is Renew's fallback once T2 passes (RFC 8415 §18.2.5).
func (c *Client) Rebind(ctx context.Context, l Lease) (Lease, error) {
	return c.extend(ctx, l, Rebind, nil, l.Expires())
}

func (c *Client) extend(ctx context.Context, l Lease, t MessageType, server []byte, until time.Time) (Lease, error) {
	ctx, cancel := context.WithDeadline(ctx, until)
	defer cancel()
	msg := Message{Type: t, ClientID: c.ClientID, ServerID: server, RequestDNS: true,
		IAPD: &IAPD{IAID: c.IAID, Prefixes: []IAPrefix{{Prefix: l.Prefix}}}}
	reply, err := c.exchange(ctx, msg, renewSchedule, func(m Message) bool { return m.Type == Reply })
	if err != nil {
		return l, err
	}
	if !usable(reply, c.IAID) {
		return l, refusal(reply)
	}
	return c.lease(reply), nil
}

// Release gives the prefix back, for when the operator stops asking for one.
// Best effort: a server that does not hear it lets the lease run out.
func (c *Client) Release(ctx context.Context, l Lease) error {
	msg := Message{Type: Release, ClientID: c.ClientID, ServerID: l.ServerID,
		IAPD: &IAPD{IAID: c.IAID, Prefixes: []IAPrefix{{Prefix: l.Prefix}}}}
	_, err := c.exchange(ctx, msg, releaseSchedule, func(m Message) bool { return m.Type == Reply })
	return err
}

// exchange sends msg and retransmits it on sched until accept takes an answer.
//
// Answers are matched on the transaction ID and our client ID before accept
// sees them, so a reply meant for another client on the segment — or for an
// earlier exchange of ours — is never mistaken for this one's.
func (c *Client) exchange(ctx context.Context, msg Message, sched schedule, accept func(Message) bool) (Message, error) {
	if _, err := rand.Read(msg.XID[:]); err != nil {
		return Message{}, err
	}
	start := c.now()
	rt := sched.irt
	for attempt := 1; ; attempt++ {
		msg.Elapsed = c.now().Sub(start)
		if attempt == 1 {
			msg.Elapsed = 0
		}
		if err := c.Conn.Send(ctx, msg.Encode()); err != nil {
			return Message{}, err
		}

		deadline := c.now().Add(rt)
		for {
			b, err := c.Conn.Receive(ctx, deadline)
			if errors.Is(err, ErrTimeout) {
				break
			}
			if err != nil {
				return Message{}, err
			}
			m, err := Decode(b)
			if err != nil || m.XID != msg.XID || string(m.ClientID) != string(msg.ClientID) {
				continue
			}
			if accept(m) {
				return m, nil
			}
		}

		if err := ctx.Err(); err != nil {
			return Message{}, err
		}
		if sched.mrc > 0 && attempt >= sched.mrc {
			return Message{}, ErrNoServer
		}
		if sched.mrd > 0 && c.now().Sub(start) >= sched.mrd {
			return Message{}, ErrNoServer
		}
		rt *= 2
		if sched.mrt > 0 && rt > sched.mrt {
			rt = sched.mrt
		}
	}
}

// usable reports whether a message carries a prefix for our IA.
func usable(m Message, iaid uint32) bool {
	if m.Status != StatusSuccess || m.IAPD == nil || m.IAPD.IAID != iaid ||
		m.IAPD.Status != StatusSuccess {
		return false
	}
	for _, p := range m.IAPD.Prefixes {
		if p.Valid > 0 && p.Prefix.Addr().Is6() {
			return true
		}
	}
	return false
}

// refusal turns a Reply without a prefix into the server's own words.
func refusal(m Message) error {
	switch {
	case m.IAPD != nil && m.IAPD.Status == StatusNoPrefixAvail:
		return fmt.Errorf("the ISP has no prefix to delegate (NoPrefixAvail%s)", suffix(m.IAPD.StatusMessage))
	case m.IAPD != nil && m.IAPD.Status != StatusSuccess:
		return fmt.Errorf("the ISP refused the prefix (status %d%s)", m.IAPD.Status, suffix(m.IAPD.StatusMessage))
	case m.Status != StatusSuccess:
		return fmt.Errorf("the ISP refused the request (status %d%s)", m.Status, suffix(m.StatusMessage))
	}
	return ErrNoServer
}

func suffix(s string) string {
	if s == "" {
		return ""
	}
	return ": " + s
}

// lease reads the lease out of a usable Reply.
//
// The first prefix still valid is the lease. An ISP delegates one; a second is
// a renumbering in progress, where the old one arrives with a zero lifetime and
// the new one beside it, and taking the valid one is taking the new one.
//
// T1 and T2 of zero leave the timing to the client, and RFC 8415 §21.21
// suggests half and eight tenths of the preferred lifetime.
func (c *Client) lease(m Message) Lease {
	l := Lease{ServerID: m.ServerID, DNS: m.DNS, Obtained: c.now(), T1: m.IAPD.T1, T2: m.IAPD.T2}
	for _, p := range m.IAPD.Prefixes {
		if p.Valid > 0 {
			l.Prefix, l.Preferred, l.Valid = p.Prefix, p.Preferred, p.Valid
			break
		}
	}
	if l.T1 <= 0 || l.T1 > l.Preferred {
		l.T1 = l.Preferred / 2
	}
	if l.T2 <= 0 || l.T2 < l.T1 {
		l.T2 = l.Preferred * 8 / 10
	}
	if l.T1 < time.Minute {
		// A lease whose lifetime rounds to nothing would have us renewing in a
		// tight loop; a minute is the floor any real server's numbers clear.
		l.T1, l.T2 = time.Minute, max(l.T2, 2*time.Minute)
	}
	return l
}
