package remote

import (
	"net/netip"
	"strings"
	"testing"
	"time"
)

// Planning compares against the kernel rather than against what we last wrote,
// so every fixture here starts by making the kernel hold a config and then
// changes the config.

// applied returns the observed state a kernel would report for c.
func applied(c Config) Observed {
	d := Render(c)
	obs := Observed{Known: true, Present: d.Enabled, Up: d.Enabled, Lines: d.Lines()}
	for _, p := range d.Peers {
		obs.Peers = append(obs.Peers, PeerState{PublicKey: p.PublicKey})
	}
	return obs
}

// connected marks a peer as having handshaked, which is what turns "removing a
// configuration nobody imported" into "revoking somebody's access".
func connected(obs Observed, key string) Observed {
	for i := range obs.Peers {
		if obs.Peers[i].PublicKey == key {
			obs.Peers[i].LastHandshake = time.Now().Add(-time.Minute)
		}
	}
	return obs
}

// planFor plans c against obs with c itself as the stored config — the
// no-edit case. planAfter is for the cases where something was removed, since
// only the stored config can still name it.
func planFor(t *testing.T, c Config, obs Observed) Plan {
	t.Helper()
	return planAfter(t, c, c, obs)
}

func planAfter(t *testing.T, c, previous Config, obs Observed) Plan {
	t.Helper()
	plan, _, err := BuildPlan(c, previous, testNetworks, obs)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	return plan
}

// The drift check (design.md §5.4): plan unchanged intent against the kernel
// holding it and the answer is nothing.
func TestAnAppliedConfigPlansEmpty(t *testing.T) {
	c, _ := withPeer(enabledConfig(), "phone", RouteHome)

	plan := planFor(t, c, applied(c))
	if !plan.Empty() {
		t.Fatalf("an applied config still has work: %+v", plan.Changes)
	}
	if plan.Impact != ImpactNone {
		t.Errorf("impact = %s, want none", plan.Impact)
	}
}

// The ordinary case, and the one that must never be dressed up as an outage:
// `wg setconf` keeps the session state of every peer whose key is unchanged.
func TestAddingAPeerIsReload(t *testing.T) {
	before, _ := withPeer(enabledConfig(), "phone", RouteHome)
	after, laptop := withPeer(before.Clone(), "laptop", RouteHome)

	plan := planFor(t, after, applied(before))
	if plan.Impact != ImpactReload {
		t.Fatalf("impact = %s, want reload: %v", plan.Impact, plan.Reasons)
	}
	if len(plan.Changes) != 1 || plan.Changes[0].Kind != ChangeAdd {
		t.Fatalf("want one added line, got %+v", plan.Changes)
	}
	if !strings.Contains(plan.Changes[0].Line, laptop.PublicKey) {
		t.Errorf("the added line is not the new peer: %q", plan.Changes[0].Line)
	}
}

func TestRemovingAPeerIsDisruptiveAndSaysWhose(t *testing.T) {
	before, phone := withPeer(enabledConfig(), "phone", RouteHome)
	obs := connected(applied(before), phone.PublicKey)

	after := before.Clone()
	after.WireGuard.RemovePeer("phone")

	plan := planAfter(t, after, before, obs)
	if plan.Impact != ImpactDisruptive {
		t.Fatalf("impact = %s, want disruptive", plan.Impact)
	}
	// The name, not the key. A confirmation dialog saying "5vVv…= loses access"
	// tells the operator nothing they can weigh — and the name is only in the
	// config the device is being removed *from*, which is why BuildPlan reads
	// both.
	if !strings.Contains(strings.Join(plan.Reasons, " "), "phone") {
		t.Errorf("the reason does not name the device: %v", plan.Reasons)
	}
}

// Removing a device that never connected is still a revocation, and still
// refused without confirmation — but the words should not claim somebody lost
// something they never had.
func TestRemovingAPeerThatNeverConnectedSaysSo(t *testing.T) {
	before, _ := withPeer(enabledConfig(), "phone", RouteHome)
	after := before.Clone()
	after.WireGuard.RemovePeer("phone")

	plan := planAfter(t, after, before, applied(before))
	if plan.Impact != ImpactDisruptive {
		t.Fatalf("impact = %s, want disruptive", plan.Impact)
	}
	if !strings.Contains(strings.Join(plan.Reasons, " "), "never connected") {
		t.Errorf("the reason overstates the loss: %v", plan.Reasons)
	}
}

// The damage here is to files that already left the box, so it is disruptive
// even though nothing disconnects at the instant it applies.
func TestRenumberingTheNetworkIsDisruptive(t *testing.T) {
	before, _ := withPeer(enabledConfig(), "phone", RouteHome)
	obs := applied(before)

	after := before.Clone()
	after.WireGuard.Subnet = netip.MustParsePrefix("10.7.0.0/24")
	// The peer's stored address has to move with it, which is what an operator
	// doing this by hand would have to do — the validator refuses the half-done
	// state.
	peer, _ := after.WireGuard.Peer("phone")
	peer.Address = netip.MustParseAddr("10.7.0.2")
	after.WireGuard.SetPeer(peer)

	plan := planFor(t, after, obs)
	if plan.Impact != ImpactDisruptive {
		t.Fatalf("impact = %s, want disruptive: %v", plan.Impact, plan.Reasons)
	}
	if !strings.Contains(strings.Join(plan.Reasons, " "), "wrong") {
		t.Errorf("the reason does not say the files on the devices are now wrong: %v", plan.Reasons)
	}
}

// A WireGuard client retries forever, so this is an interruption rather than a
// loss — as long as the endpoint names the port, which is the case that decides
// whether the files already handed out are still right.
// With `public_port` pinned, moving the listen port changes nothing a client
// can see — a router in front is still forwarding the same number.
func TestMovingThePortIsRestartWhenTheDialledPortIsPinned(t *testing.T) {
	before, _ := withPeer(enabledConfig(), "phone", RouteHome)
	before.WireGuard.PublicPort = 51820
	obs := applied(before)

	after := before.Clone()
	after.WireGuard.ListenPort = 51821

	plan := planAfter(t, after, before, obs)
	if plan.Impact != ImpactRestart {
		t.Fatalf("impact = %s, want restart: %v", plan.Impact, plan.Reasons)
	}
}

// Without it, the port a client dials moves with the listen port, so every
// configuration already handed out points at the old one.
func TestMovingThePortIsDisruptiveWhenTheDialledPortMoves(t *testing.T) {
	before, _ := withPeer(enabledConfig(), "phone", RouteHome)
	obs := applied(before)

	after := before.Clone()
	after.WireGuard.ListenPort = 51821

	plan := planAfter(t, after, before, obs)
	if plan.Impact != ImpactDisruptive {
		t.Fatalf("impact = %s, want disruptive: %v", plan.Impact, plan.Reasons)
	}
	if !strings.Contains(strings.Join(plan.Reasons, " "), "address devices dial") {
		t.Errorf("the reason does not say what moved: %v", plan.Reasons)
	}
}

func TestReplacingThisBoxsKeyIsDisruptive(t *testing.T) {
	before, _ := withPeer(enabledConfig(), "phone", RouteHome)
	obs := applied(before)

	after := before.Clone()
	after.WireGuard.PrivateKey = mustKey().Private

	plan := planFor(t, after, obs)
	if plan.Impact != ImpactDisruptive {
		t.Fatalf("impact = %s, want disruptive: %v", plan.Impact, plan.Reasons)
	}
	if !strings.Contains(strings.Join(plan.Reasons, " "), "new file") {
		t.Errorf("the reason does not say every peer has to be re-issued: %v", plan.Reasons)
	}
}

func TestTurningItOffIsDisruptive(t *testing.T) {
	before, _ := withPeer(enabledConfig(), "phone", RouteHome)
	obs := applied(before)

	after := before.Clone()
	after.WireGuard.Enabled = false

	plan := planFor(t, after, obs)
	if plan.Impact != ImpactDisruptive {
		t.Fatalf("impact = %s, want disruptive", plan.Impact)
	}
}

// design.md §3.4's adopt-only rule, in the one place it can be violated by
// accident: `wg0` is a conventional name.
func TestAForeignInterfaceBlocksTheWholePlan(t *testing.T) {
	c := enabledConfig()

	plan := planFor(t, c, Observed{Known: true, Present: true, Foreign: true})
	if plan.Blocked == "" {
		t.Fatal("olr was willing to configure somebody else's interface")
	}
	if !strings.Contains(plan.Blocked, "--interface") {
		t.Errorf("the refusal names no way forward: %q", plan.Blocked)
	}
}

// Tearing our own tunnel down never conflicts with anybody, and refusing to
// would leave an operator unable to back out of the situation the refusal is
// about.
func TestAForeignInterfaceDoesNotBlockTurningOff(t *testing.T) {
	c := enabledConfig()
	c.WireGuard.Enabled = false

	if plan := planFor(t, c, Observed{Known: true, Present: true, Foreign: true}); plan.Blocked != "" {
		t.Errorf("turning remote access off was blocked: %q", plan.Blocked)
	}
}

// "We could not tell" and "there is nothing there" are different answers.
// Treating the first as the second would make every developer's laptop report
// that the tunnel had been torn down behind olr's back.
func TestAnUnreadableKernelPlansNothing(t *testing.T) {
	c, _ := withPeer(enabledConfig(), "phone", RouteHome)

	plan := planFor(t, c, Observed{Known: false})
	if !plan.Empty() {
		t.Fatalf("a kernel we cannot read produced work: %+v", plan.Changes)
	}
}

func TestAnInvalidConfigIsRefusedBeforePlanning(t *testing.T) {
	c := enabledConfig()
	c.Endpoint = ""

	if _, _, err := BuildPlan(c, c, testNetworks, Observed{Known: true}); err == nil {
		t.Fatal("an unapplyable config was planned")
	}
}
