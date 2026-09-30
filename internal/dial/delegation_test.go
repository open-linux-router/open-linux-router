package dial

import (
	"context"
	"net"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/dial/dhcpv6"
)

func pdConfig() Config {
	c := uplinkConfig()
	c.Uplink.IPv6 = &UplinkIPv6{Via: ViaPD, PrefixLength: 60}
	return c
}

// fakeISP delegates a prefix to whatever solicits it, and records releases.
type fakeISP struct {
	mu       sync.Mutex
	prefix   netip.Prefix
	pending  [][]byte
	released []netip.Prefix
}

func (f *fakeISP) Send(_ context.Context, b []byte) error {
	m, err := dhcpv6.Decode(b)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if m.Type == dhcpv6.Release {
		f.released = append(f.released, m.IAPD.Prefixes[0].Prefix)
	}
	reply := dhcpv6.Message{Type: dhcpv6.Reply, XID: m.XID, ClientID: m.ClientID, RapidCommit: true,
		ServerID: []byte{0, 1},
		IAPD: &dhcpv6.IAPD{IAID: IAID, T1: time.Hour, T2: 2 * time.Hour, Prefixes: []dhcpv6.IAPrefix{{
			Prefix: f.prefix, Preferred: 3 * time.Hour, Valid: 4 * time.Hour}}}}
	f.pending = append(f.pending, reply.Encode())
	return nil
}

func (f *fakeISP) Receive(ctx context.Context, deadline time.Time) ([]byte, error) {
	f.mu.Lock()
	if len(f.pending) > 0 {
		b := f.pending[0]
		f.pending = f.pending[1:]
		f.mu.Unlock()
		return b, nil
	}
	f.mu.Unlock()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(10 * time.Millisecond):
		return nil, dhcpv6.ErrTimeout
	}
}

func (f *fakeISP) Close() error { return nil }

func newTestDelegation(isp *fakeISP) (*Delegation, chan [2]netip.Prefix) {
	changes := make(chan [2]netip.Prefix, 8)
	return &Delegation{
		Listen:  func(string) (DHCPv6Conn, error) { return isp, nil },
		MAC:     func(string) (net.HardwareAddr, error) { return net.ParseMAC("52:54:00:12:34:56") },
		Changed: func(old, next netip.Prefix) { changes <- [2]netip.Prefix{old, next} },
		Retry:   10 * time.Millisecond,
	}, changes
}

func waitChange(t *testing.T, ch chan [2]netip.Prefix) [2]netip.Prefix {
	t.Helper()
	select {
	case c := <-ch:
		return c
	case <-time.After(5 * time.Second):
		t.Fatal("no prefix change announced")
	}
	return [2]netip.Prefix{}
}

// The whole loop in one: asked for on a config that wants it, announced when
// it arrives, given back and withdrawn when the config stops asking.
func TestDelegationFollowsTheConfig(t *testing.T) {
	isp := &fakeISP{prefix: netip.MustParsePrefix("2408:8000:1234:5670::/60")}
	d, changes := newTestDelegation(isp)

	d.Watch(context.Background(), pdConfig())
	got := waitChange(t, changes)
	if got[0].IsValid() || got[1] != isp.prefix {
		t.Fatalf("change = %v, want none → %s", got, isp.prefix)
	}
	if st, ok := d.State(); !ok || st.Phase != PhaseBound || st.Prefix != isp.prefix {
		t.Errorf("state = %+v", st)
	}

	// An edit that does not touch prefix delegation leaves the lease alone.
	other := pdConfig()
	other.Uplink.DNS = []netip.Addr{netip.MustParseAddr("192.168.2.1")}
	d.Watch(context.Background(), other)
	select {
	case c := <-changes:
		t.Fatalf("an unrelated edit restarted delegation: %v", c)
	case <-time.After(50 * time.Millisecond):
	}

	d.Watch(context.Background(), uplinkConfig())
	got = waitChange(t, changes)
	if got[0] != isp.prefix || got[1].IsValid() {
		t.Errorf("change = %v, want %s → none", got, isp.prefix)
	}
	if len(isp.released) != 1 || isp.released[0] != isp.prefix {
		t.Errorf("released %v", isp.released)
	}
	if _, ok := d.State(); ok {
		t.Error("state still reported after delegation stopped")
	}
}

// olrd shutting down is not the operator giving the prefix back: the lease
// outlives the process and the networks keep their addresses.
func TestShuttingDownKeepsThePrefix(t *testing.T) {
	isp := &fakeISP{prefix: netip.MustParsePrefix("2408:8000:1234:5670::/60")}
	d, changes := newTestDelegation(isp)
	ctx, cancel := context.WithCancel(context.Background())
	d.Watch(ctx, pdConfig())
	waitChange(t, changes)

	cancel()
	select {
	case c := <-changes:
		t.Errorf("shutdown withdrew the prefix: %v", c)
	case <-time.After(100 * time.Millisecond):
	}
	if len(isp.released) != 0 {
		t.Errorf("shutdown released %v", isp.released)
	}
}

func TestPDValidation(t *testing.T) {
	c := pdConfig()
	if res := Validate(c, uplinkLinks()); !res.OK() {
		t.Fatalf("errors: %v", res.Errors)
	}
	if res := Validate(c, uplinkLinks()); !hasProblem(res.Warnings, "uplink.ipv6", "delegate a prefix onward") {
		t.Errorf("a private uplink should warn about the router in front: %v", res.Warnings)
	}
	c.Uplink.IPv6.PrefixLength = 40
	if res := Validate(c, uplinkLinks()); !hasProblem(res.Errors, "uplink.ipv6.prefix_length", "") {
		t.Errorf("a /40 hint validated: %v", res.Errors)
	}
	c = pdConfig()
	c.Uplink.IPv6.Server = netip.MustParseAddr("216.66.80.26")
	if res := Validate(c, uplinkLinks()); !hasProblem(res.Errors, "uplink.ipv6", "tunnel") {
		t.Errorf("tunnel fields on prefix delegation validated: %v", res.Errors)
	}
}

// accept_ra 2 is the uplink's half of prefix delegation, planned when it
// reads anything else.
func TestPDPlansAcceptRAOnTheUplink(t *testing.T) {
	obs := agreeingUplink()
	obs.AcceptRA = "1"
	plan := buildPlan(uplinkConfig(), pdConfig(), testLinks(), obs)
	if !strings.Contains(planDiff(plan, "interfaces[enp2s0]"), "accept_ra = 2") {
		t.Errorf("no accept_ra in %+v", plan.Changes)
	}
	obs.AcceptRA = "2"
	if strings.Contains(planDiff(buildPlan(pdConfig(), pdConfig(), testLinks(), obs), "interfaces[enp2s0]"), "accept_ra") {
		t.Error("accept_ra already 2 was planned again")
	}
}
