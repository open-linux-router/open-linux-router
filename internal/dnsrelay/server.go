package dnsrelay

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"sync"
	"time"
)

// The transports. Both call the same resolve, and both follow the same order:
// serve the client first, observe second.

// maxMessage is the largest DNS message either transport will handle. It is the
// protocol's own ceiling — a TCP message carries a 16-bit length prefix — so
// nothing legitimate exceeds it.
const maxMessage = 65535

// tcpIdleTimeout bounds how long a client may hold a connection open between
// queries.
//
// DNS over TCP is connection-reuse-friendly by design (RFC 7766), so this is
// generous rather than tight; it exists to reap abandoned connections, not to
// discourage reuse.
const tcpIdleTimeout = 30 * time.Second

// Run binds every listener and serves until the context is cancelled.
//
// ready is called once every socket is bound and the relay is genuinely
// answering. That is what makes Type=notify worth having on the unit: the
// nftables redirect is loaded in ExecStartPost, and loading it before the relay
// could answer would point the whole network's DNS at a closed port.
func (r *Relay) Run(ctx context.Context, ready func()) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		wg      sync.WaitGroup
		closers []io.Closer
	)
	fail := func(err error) error {
		for _, c := range closers {
			c.Close()
		}
		return err
	}

	for _, addr := range r.cfg.Listen {
		udp, err := net.ListenUDP("udp", net.UDPAddrFromAddrPort(addr))
		if err != nil {
			return fail(fmt.Errorf("listening on %s/udp: %w", addr, err))
		}
		closers = append(closers, udp)

		tcp, err := net.ListenTCP("tcp", net.TCPAddrFromAddrPort(addr))
		if err != nil {
			return fail(fmt.Errorf("listening on %s/tcp: %w", addr, err))
		}
		closers = append(closers, tcp)

		r.logger.Info("listening", "address", addr, "upstream", r.cfg.Upstream)

		wg.Add(2)
		go func() { defer wg.Done(); r.serveUDP(ctx, udp) }()
		go func() { defer wg.Done(); r.serveTCP(ctx, tcp) }()
	}

	// The observation side and the read-only API both live behind the tee and
	// neither can affect an answer.
	wg.Add(1)
	go func() { defer wg.Done(); r.observeLoop(ctx) }()

	var api *apiServer
	if r.cfg.ObserveSocket != "" {
		var err error
		api, err = r.serveObservations(ctx)
		if err != nil {
			// Not fatal. Losing the query log is a lost feature; refusing to
			// resolve because of it would be an outage, and the two are not
			// close to comparable.
			r.logger.Error("could not serve observations; the query log will be unreadable",
				"socket", r.cfg.ObserveSocket, "error", err)
		}
	}

	if ready != nil {
		ready()
	}

	<-ctx.Done()
	for _, c := range closers {
		c.Close()
	}
	if api != nil {
		api.Close()
	}
	wg.Wait()
	return nil
}

func (r *Relay) serveUDP(ctx context.Context, conn *net.UDPConn) {
	// One buffer for this listener's read loop, reused every iteration. That is
	// exactly why Relay.observe copies before handing anything to the tee.
	buf := make([]byte, maxMessage)

	for {
		n, from, err := conn.ReadFromUDPAddrPort(buf)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return
			}
			r.logger.Warn("udp read failed", "error", err)
			continue
		}

		client := from.Addr()
		r.counters.Queries.Add(1)
		if !r.allowed(client) {
			// Dropped without an answer, not refused with one. A REFUSED reply
			// is still a reply, and answering an unsolicited source at all is
			// what makes a resolver useful as a reflector.
			r.counters.Refused.Add(1)
			continue
		}
		r.clients.Seen(client, time.Now())

		// Copied before the goroutine, because the loop reuses buf on the very
		// next iteration.
		query := make([]byte, n)
		copy(query, buf[:n])

		go func() {
			at := time.Now()
			res, err := r.resolve(ctx, client, query, false)
			if err != nil {
				r.logger.Warn("could not resolve", "client", client, "error", err)
				return
			}
			// The client is served here, before anything is observed.
			if _, err := conn.WriteToUDPAddrPort(res.response, from); err != nil {
				r.logger.Warn("udp write failed", "client", client, "error", err)
				return
			}
			r.observe(client, res, at)
		}()
	}
}

func (r *Relay) serveTCP(ctx context.Context, ln *net.TCPListener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return
			}
			r.logger.Warn("tcp accept failed", "error", err)
			continue
		}

		client, _ := netip.AddrFromSlice(conn.RemoteAddr().(*net.TCPAddr).IP)
		if !r.allowed(client) {
			r.counters.Queries.Add(1)
			r.counters.Refused.Add(1)
			conn.Close()
			continue
		}
		go r.serveTCPConn(ctx, conn, client)
	}
}

func (r *Relay) serveTCPConn(ctx context.Context, conn net.Conn, client netip.Addr) {
	defer conn.Close()

	for {
		if err := conn.SetReadDeadline(time.Now().Add(tcpIdleTimeout)); err != nil {
			return
		}
		query, err := readTCPMessage(conn)
		if err != nil {
			// Includes the ordinary end of a connection, so this is not logged.
			return
		}

		r.counters.Queries.Add(1)
		r.clients.Seen(client, time.Now())

		at := time.Now()
		res, err := r.resolve(ctx, client, query, true)
		if err != nil {
			r.logger.Warn("could not resolve", "client", client, "error", err)
			return
		}
		if err := conn.SetWriteDeadline(time.Now().Add(tcpIdleTimeout)); err != nil {
			return
		}
		if err := writeTCPMessage(conn, res.response); err != nil {
			return
		}
		r.observe(client, res, at)
	}
}

// forward relays a query to the resolver behind us and returns its answer
// verbatim.
//
// The response is never parsed here and never rebuilt. Re-serialising would
// break DNSSEC signatures, drop EDNS options we did not model, and mangle
// record types we have never heard of — see message.go.
func (r *Relay) forward(ctx context.Context, query []byte, overTCP bool) ([]byte, error) {
	if overTCP {
		// A client that asked over TCP is usually expecting an answer too large
		// for a datagram, so the transport is preserved rather than downgraded.
		return r.forwardTCP(ctx, query)
	}
	return r.forwardUDP(ctx, query)
}

func (r *Relay) forwardUDP(ctx context.Context, query []byte) ([]byte, error) {
	// One socket per query, and no response-demultiplexing table anywhere.
	//
	// A shared connected socket would need queries keyed by transaction ID,
	// a timeout sweeper, and a story for ID collisions. The upstream is unbound
	// on loopback and the load is a house — tens of queries a second — so a
	// socket per query costs an ephemeral port for a few milliseconds and
	// removes an entire class of correlation bug.
	var d net.Dialer
	conn, err := d.DialContext(ctx, "udp", r.cfg.Upstream.String())
	if err != nil {
		return nil, fmt.Errorf("dialling the resolver at %s: %w", r.cfg.Upstream, err)
	}
	defer conn.Close()

	deadline := time.Now().Add(r.cfg.Timeout())
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, err
	}
	if _, err := conn.Write(query); err != nil {
		return nil, fmt.Errorf("sending to the resolver: %w", err)
	}

	buf := make([]byte, maxMessage)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, fmt.Errorf("reading from the resolver at %s: %w", r.cfg.Upstream, err)
	}
	return buf[:n], nil
}

func (r *Relay) forwardTCP(ctx context.Context, query []byte) ([]byte, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", r.cfg.Upstream.String())
	if err != nil {
		return nil, fmt.Errorf("dialling the resolver at %s: %w", r.cfg.Upstream, err)
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(r.cfg.Timeout())); err != nil {
		return nil, err
	}
	if err := writeTCPMessage(conn, query); err != nil {
		return nil, fmt.Errorf("sending to the resolver: %w", err)
	}
	response, err := readTCPMessage(conn)
	if err != nil {
		return nil, fmt.Errorf("reading from the resolver at %s: %w", r.cfg.Upstream, err)
	}
	return response, nil
}

// readTCPMessage reads one length-prefixed DNS message (RFC 1035 §4.2.2).
func readTCPMessage(conn net.Conn) ([]byte, error) {
	var length [2]byte
	if _, err := io.ReadFull(conn, length[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint16(length[:])
	if n == 0 {
		return nil, errors.New("zero-length message")
	}
	msg := make([]byte, n)
	if _, err := io.ReadFull(conn, msg); err != nil {
		return nil, err
	}
	return msg, nil
}

// writeTCPMessage writes one length-prefixed DNS message.
func writeTCPMessage(conn net.Conn, msg []byte) error {
	if len(msg) > maxMessage {
		return fmt.Errorf("message of %d bytes exceeds the %d byte limit", len(msg), maxMessage)
	}
	// One write, not two. A separate write for the prefix produces a two-packet
	// answer that some clients handle poorly, and there is no reason to.
	out := make([]byte, 2+len(msg))
	binary.BigEndian.PutUint16(out[:2], uint16(len(msg)))
	copy(out[2:], msg)
	_, err := conn.Write(out)
	return err
}
