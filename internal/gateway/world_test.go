package gateway

import (
	"errors"
	"fmt"
	"net/netip"
	"testing"
	"time"
)

func TestWorldWindow(t *testing.T) {
	m := NewWorldTraffic(nil)
	now := time.Unix(1000, 0)
	ip := netip.MustParseAddr("8.8.8.8")
	m.sample(now, []worldFlow{{Key: "a", IP: ip, Up: 10000, Down: 20000}}, true, nil)
	if m.snapshot.State != "warming" {
		t.Fatal(m.snapshot)
	}
	m.sample(now.Add(2*time.Second), []worldFlow{{Key: "a", IP: ip, Up: 10200, Down: 20600}, {Key: "b", IP: ip, Up: 100, Down: 200}}, true, nil)
	s := m.snapshot
	if len(s.Peers) != 1 || s.Peers[0].Upload != 150 || s.Peers[0].Download != 400 || s.Total != 550 {
		t.Fatal(s)
	}
	// A counter reset does not underflow. Disappearing connections never
	// subtract previously observed bytes from the retained window.
	m.sample(now.Add(4*time.Second), []worldFlow{{Key: "a", IP: ip, Up: 1, Down: 2}}, true, nil)
	if m.snapshot.Total != 275 {
		t.Fatal(m.snapshot)
	}
	m.sample(now.Add(34*time.Second), nil, true, nil)
	if m.snapshot.Total != 0 || len(m.snapshot.Peers) != 0 {
		t.Fatal(m.snapshot)
	}
	m.sample(now.Add(36*time.Second), nil, false, nil)
	if m.snapshot.State != "disabled" || m.previous != nil {
		t.Fatal(m.snapshot)
	}
	m.sample(now.Add(38*time.Second), nil, true, errors.New("denied"))
	if m.snapshot.State != "unavailable" || m.snapshot.Error != "denied" {
		t.Fatal(m.snapshot)
	}
}

func TestWorldTopTenKeepsFullDenominator(t *testing.T) {
	m := NewWorldTraffic(nil)
	now := time.Unix(1000, 0)
	m.sample(now, nil, true, nil)
	var flows []worldFlow
	for i := 1; i <= 20; i++ {
		flows = append(flows, worldFlow{Key: fmt.Sprint(i), IP: netip.MustParseAddr(fmt.Sprintf("8.8.8.%d", i)), Up: uint64(i * 2)})
	}
	m.sample(now.Add(2*time.Second), flows, true, nil)
	if len(m.snapshot.Peers) != 10 || m.snapshot.Total != 210 || m.snapshot.Peers[0].Upload != 20 || m.snapshot.Peers[9].Upload != 11 {
		t.Fatal(m.snapshot)
	}
}

func TestWorldAccountingRendering(t *testing.T) {
	on := Render(Config{}, nil, nil)
	found := false
	for _, s := range on.Sysctls {
		if s.Key == "net.netfilter.nf_conntrack_acct" && s.Value == "1" {
			found = true
		}
	}
	if !found {
		t.Fatal("statistics must enable connection accounting even without routing")
	}
	disabled := false
	off := Render(Config{Stats: &disabled}, nil, nil)
	for _, s := range off.Sysctls {
		if s.Key == "net.netfilter.nf_conntrack_acct" {
			t.Fatal("must not disable accounting used by other modules")
		}
	}
}
