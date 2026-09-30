package dial

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/dial/dhcpv6"
)

// The prefix-delegation loop: get a prefix from the ISP, keep it, and say when
// it changes.
//
// It lives inside olrd by design.md §3.5's test, like the publisher: with olrd
// down, nothing renews the lease — and the addresses link wrote from it stay
// put until somebody restarts olrd, which renews or replaces them. The one
// fact that has to survive is the prefix the ISP keys on (DUID, IAID), and
// both are functions of the uplink's MAC, so there is nothing to persist.

// Phases of the loop, as status reports them.
const (
	PhaseSoliciting = "soliciting"
	PhaseBound      = "bound"
	PhaseRenewing   = "renewing"
	PhaseRebinding  = "rebinding"
	PhaseWaiting    = "waiting"
)

// IAID is the one identity association olr asks for. Fixed, because the ISP
// keys the delegation on it and a changing number is a changing prefix.
const IAID = 1

// DelegationState is what status knows about the delegated prefix.
type DelegationState struct {
	// Interface is the uplink it is being asked for on.
	Interface string `json:"interface"`

	// Phase is where the loop is: soliciting, bound, renewing, rebinding, or
	// waiting after a failure.
	Phase string `json:"phase"`

	// Prefix is the delegated prefix, invalid while there is none.
	Prefix netip.Prefix `json:"prefix,omitzero"`

	// Obtained is when the current lease was granted or last renewed, and
	// Renews and Expires are when it will be renewed and when it runs out.
	Obtained time.Time `json:"obtained,omitzero"`
	Renews   time.Time `json:"renews,omitzero"`
	Expires  time.Time `json:"expires,omitzero"`

	// DNS are the resolvers the ISP offered with it.
	DNS []netip.Addr `json:"dns,omitempty"`

	// Error is why the last attempt failed, empty when it did not.
	Error string `json:"error,omitempty"`
}

// Delegation runs the loop for whichever uplink the config asks it for.
//
// Watch shape as the Publisher's, and for the same reason: it follows a config
// the operator keeps editing, and an edit that does not touch prefix
// delegation must not restart it — a restart is a new Solicit, and on some
// ISPs a new prefix.
type Delegation struct {
	// Listen opens the client socket. Nil means dhcpv6.Listen.
	Listen func(iface string) (DHCPv6Conn, error)

	// MAC reads the uplink's hardware address, for the DUID. Nil means the
	// kernel's.
	MAC func(iface string) (net.HardwareAddr, error)

	// Changed is called with the previous and the new prefix whenever the
	// prefix appears, moves or goes. Either may be invalid. Not called on a
	// renewal that keeps the prefix.
	//
	// It must not block on the apply lock. Watch runs under that lock and waits
	// for a stopping loop, which announces its prefix going on the way out — so
	// a Changed that took the lock itself would wait on the apply that is
	// waiting on it. olrd hands the work to a goroutine that reconciles against
	// Prefix() when it gets the lock, which also makes the order two quick
	// announcements arrive in not matter.
	Changed func(old, next netip.Prefix)

	// Retry is how long to wait after a failure. Zero means 30 seconds.
	Retry time.Duration

	Log *slog.Logger

	mu      sync.Mutex
	state   DelegationState
	running *delegationKey
	cancel  context.CancelFunc
	done    chan struct{}

	// withdrawing is set by Watch before it stops a loop, and tells the loop
	// to give its prefix back. A loop stopped any other way — olrd shutting
	// down — keeps it: the ISP's lease outlives the process, the addresses
	// numbered from it stay on the networks, and the next start renews it.
	withdrawing bool
}

// DHCPv6Conn is the socket the loop talks through.
type DHCPv6Conn interface {
	dhcpv6.Conn
	Close() error
}

type delegationKey struct {
	iface string
	hint  int
}

// State returns what is known about the delegated prefix, and false when
// nothing is being delegated.
func (d *Delegation) State() (DelegationState, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.running == nil {
		return DelegationState{}, false
	}
	s := d.state
	s.DNS = append([]netip.Addr(nil), s.DNS...)
	return s, true
}

// Prefix is the delegated prefix, invalid when there is none.
func (d *Delegation) Prefix() netip.Prefix {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.state.Prefix
}

// Watch starts, restarts or stops the loop to match c.
func (d *Delegation) Watch(ctx context.Context, c Config) {
	var want *delegationKey
	if c.Uplink.HasPD() {
		want = &delegationKey{iface: c.Uplink.Interface, hint: c.Uplink.IPv6.PrefixLength}
	}

	d.mu.Lock()
	if (want == nil && d.running == nil) || (want != nil && d.running != nil && *want == *d.running) {
		d.mu.Unlock()
		return
	}
	cancel, done := d.cancel, d.done
	d.mu.Unlock()

	// Stop the old loop first, and wait for it: it releases its lease and
	// withdraws its prefix on the way out, and a new loop starting before that
	// finished could see the prefix it is about to be given withdrawn under it.
	if cancel != nil {
		d.mu.Lock()
		d.withdrawing = true
		d.mu.Unlock()
		cancel()
		<-done
	}

	d.mu.Lock()
	d.withdrawing = false
	d.running, d.cancel, d.done = want, nil, nil
	d.state = DelegationState{}
	if want != nil {
		d.state = DelegationState{Interface: want.iface, Phase: PhaseSoliciting}
		child, cancel := context.WithCancel(ctx)
		d.cancel, d.done = cancel, make(chan struct{})
		go d.run(child, *want, d.done)
	}
	d.mu.Unlock()
}

func (d *Delegation) run(ctx context.Context, key delegationKey, done chan struct{}) {
	defer close(done)
	var lease dhcpv6.Lease
	var client *dhcpv6.Client
	var conn DHCPv6Conn

	defer func() {
		d.mu.Lock()
		withdraw := d.withdrawing
		d.mu.Unlock()
		if !withdraw {
			if conn != nil {
				conn.Close()
			}
			return
		}
		if conn != nil {
			if lease.Prefix.IsValid() && client != nil {
				// Best effort and brief: a server that does not hear this lets
				// the lease run out, and the operator is waiting on the apply.
				rctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				_ = client.Release(rctx, lease)
				cancel()
			}
			conn.Close()
		}
		d.setPrefix(netip.Prefix{})
	}()

	for ctx.Err() == nil {
		if conn == nil {
			c, cl, err := d.open(key)
			if err != nil {
				d.fail(ctx, err)
				continue
			}
			conn, client = c, cl
		}

		d.update(func(s *DelegationState) { s.Phase, s.Error = PhaseSoliciting, "" })
		l, err := client.Acquire(ctx)
		if err != nil {
			// A fresh socket for the next attempt: the interface may have gone
			// and come back, and a socket bound to its old link-local address
			// would go on sending into nothing.
			conn.Close()
			conn, client = nil, nil
			d.fail(ctx, err)
			continue
		}
		lease = l
		d.bound(lease)

		// Keep it: renew at T1, rebind at T2, start over when it expires.
		for ctx.Err() == nil {
			if !sleepUntil(ctx, lease.Obtained.Add(lease.T1)) {
				return
			}
			d.update(func(s *DelegationState) { s.Phase = PhaseRenewing })
			next, err := client.Renew(ctx, lease)
			if err != nil && ctx.Err() == nil {
				d.update(func(s *DelegationState) { s.Phase, s.Error = PhaseRebinding, err.Error() })
				next, err = client.Rebind(ctx, lease)
			}
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				d.logf("the delegated prefix expired without being renewed", "prefix", lease.Prefix, "error", err)
				lease = dhcpv6.Lease{}
				d.setPrefix(netip.Prefix{})
				break
			}
			lease = next
			d.bound(lease)
		}
	}
}

// open binds the socket and builds the client for the uplink.
func (d *Delegation) open(key delegationKey) (DHCPv6Conn, *dhcpv6.Client, error) {
	mac, err := d.mac(key.iface)
	if err != nil {
		return nil, nil, err
	}
	conn, err := d.listen(key.iface)
	if err != nil {
		return nil, nil, err
	}
	return conn, &dhcpv6.Client{Conn: conn, ClientID: dhcpv6.DUIDLL(mac), IAID: IAID, Hint: key.hint}, nil
}

func (d *Delegation) listen(iface string) (DHCPv6Conn, error) {
	if d.Listen != nil {
		return d.Listen(iface)
	}
	return dhcpv6.Listen(iface)
}

func (d *Delegation) mac(iface string) (net.HardwareAddr, error) {
	if d.MAC != nil {
		return d.MAC(iface)
	}
	ifi, err := net.InterfaceByName(iface)
	if err != nil {
		return nil, err
	}
	if len(ifi.HardwareAddr) == 0 {
		return nil, errors.New(iface + " has no hardware address to build a DHCPv6 identity from")
	}
	return ifi.HardwareAddr, nil
}

// bound records a lease, and announces the prefix if it moved.
func (d *Delegation) bound(l dhcpv6.Lease) {
	d.update(func(s *DelegationState) {
		s.Phase, s.Error = PhaseBound, ""
		s.Obtained, s.Renews, s.Expires = l.Obtained, l.Obtained.Add(l.T1), l.Expires()
		s.DNS = l.DNS
	})
	d.setPrefix(l.Prefix)
}

// setPrefix changes the published prefix and calls Changed when it moved.
func (d *Delegation) setPrefix(p netip.Prefix) {
	d.mu.Lock()
	old := d.state.Prefix
	d.state.Prefix = p
	if !p.IsValid() {
		d.state.Obtained, d.state.Renews, d.state.Expires, d.state.DNS = time.Time{}, time.Time{}, time.Time{}, nil
	}
	d.mu.Unlock()
	if old == p {
		return
	}
	d.logf("delegated prefix changed", "from", old, "to", p)
	if d.Changed != nil {
		d.Changed(old, p)
	}
}

// fail records why the last attempt failed and waits before the next.
func (d *Delegation) fail(ctx context.Context, err error) {
	if ctx.Err() != nil {
		return
	}
	d.update(func(s *DelegationState) { s.Phase, s.Error = PhaseWaiting, err.Error() })
	d.logf("prefix delegation failed; retrying", "error", err)
	retry := d.Retry
	if retry == 0 {
		retry = 30 * time.Second
	}
	sleepUntil(ctx, time.Now().Add(retry))
}

func (d *Delegation) update(fn func(*DelegationState)) {
	d.mu.Lock()
	fn(&d.state)
	d.mu.Unlock()
}

func (d *Delegation) logf(msg string, args ...any) {
	if d.Log != nil {
		d.Log.Info(msg, args...)
	}
}

// sleepUntil waits for t or ctx, and reports whether it was t.
func sleepUntil(ctx context.Context, t time.Time) bool {
	timer := time.NewTimer(time.Until(t))
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}
