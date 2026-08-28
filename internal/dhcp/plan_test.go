package dhcp

import (
	"strings"
	"testing"
	"time"
)

var planNow = time.Unix(1767225000, 0).UTC()

// observe renders a config and presents the result as the current on-disk
// state, i.e. "this config is exactly what is running".
func observe(t *testing.T, c Config, running bool, leases ...Lease) Observed {
	t.Helper()
	rendered, err := NewDnsmasq(DefaultPaths()).Render(c, testLinks())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	files := map[string][]byte{}
	for _, f := range rendered.Files {
		files[f.Path] = f.Data
	}
	return Observed{Files: files, Running: running, Leases: leases}
}

func buildPlan(t *testing.T, desired Config, obs Observed) Plan {
	t.Helper()
	plan, err := BuildPlan(NewDnsmasq(DefaultPaths()), desired, testLinks(), obs, planNow)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	return plan
}

func activeLease(t *testing.T, ip, mac, hostname string) Lease {
	t.Helper()
	return Lease{IP: addr(t, ip), MAC: mac, Hostname: hostname, Expires: planNow.Add(time.Hour)}
}

// Drift detection is exactly "plan against unchanged intent and see if the diff
// is empty" (design.md §5.4), so this is the case that has to be right before
// any of the others matter.
func TestPlanIsEmptyWhenNothingChanged(t *testing.T) {
	c := validConfig(t)
	plan := buildPlan(t, c, observe(t, c, true))

	if !plan.Empty() {
		t.Errorf("expected no work, got %d changes and action %q", len(plan.Changes), plan.Action)
	}
	if plan.Impact != ImpactNone {
		t.Errorf("Impact = %s, want none", plan.Impact)
	}
}

// The whole point of the hosts.d/opts.d split: the most common edit must not
// bounce the daemon.
func TestAddingAReservationOnlyReloads(t *testing.T) {
	current := validConfig(t)
	obs := observe(t, current, true)

	desired := current.Clone()
	desired.SetReservation(Reservation{MAC: "aa:bb:cc:dd:ee:ff", IP: addr(t, "192.168.1.50"), Hostname: "nas"})

	plan := buildPlan(t, desired, obs)

	if plan.Action != ActionReload {
		t.Errorf("Action = %q, want reload", plan.Action)
	}
	if plan.Impact != ImpactReload {
		t.Errorf("Impact = %s, want reload", plan.Impact)
	}
	if len(plan.Changes) != 1 {
		t.Fatalf("expected exactly one file to change, got %v", changePaths(plan))
	}
	if plan.Changes[0].Kind != ChangeCreate {
		t.Errorf("Kind = %q, want create", plan.Changes[0].Kind)
	}
	if !strings.HasPrefix(plan.Changes[0].Path, DefaultPaths().HostsDir) {
		t.Errorf("reservation written to %q, outside the reloadable directory", plan.Changes[0].Path)
	}
}

// dnsmasq picks up a *new* file in hostsdir on its own but keeps a deleted
// record until SIGHUP. So a removal must still be planned as a reload —
// otherwise the reservation would silently stay in force.
func TestRemovingAReservationDeletesTheFileAndReloads(t *testing.T) {
	current := validConfig(t)
	current.SetReservation(Reservation{MAC: "aa:bb:cc:dd:ee:ff", IP: addr(t, "192.168.1.50")})
	obs := observe(t, current, true)

	desired := current.Clone()
	desired.RemoveReservation("aa:bb:cc:dd:ee:ff")

	plan := buildPlan(t, desired, obs)

	if len(plan.Changes) != 1 {
		t.Fatalf("expected one change, got %v", changePaths(plan))
	}
	if plan.Changes[0].Kind != ChangeDelete {
		t.Errorf("Kind = %q, want delete", plan.Changes[0].Kind)
	}
	if plan.Action != ActionReload {
		t.Errorf("Action = %q, want reload; a deleted host record needs SIGHUP", plan.Action)
	}
}

// A stale file nobody's config produces any more must still be noticed: dnsmasq
// reads whatever is in the directory, not whatever we meant to put there.
func TestStrayRenderedFileIsPlannedForDeletion(t *testing.T) {
	c := validConfig(t)
	obs := observe(t, c, true)
	stray := DefaultPaths().HostsDir + "/deadbeef0000.conf"
	obs.Files[stray] = []byte("de:ad:be:ef:00:00,192.168.1.77\n")

	plan := buildPlan(t, c, obs)

	if len(plan.Changes) != 1 || plan.Changes[0].Path != stray {
		t.Fatalf("stray file not planned for removal; got %v", changePaths(plan))
	}
	if plan.Changes[0].Kind != ChangeDelete {
		t.Errorf("Kind = %q, want delete", plan.Changes[0].Kind)
	}
}

// Changing a pool touches the main config, which dnsmasq never re-reads.
func TestChangingAPoolRestarts(t *testing.T) {
	current := validConfig(t)
	obs := observe(t, current, true, activeLease(t, "192.168.1.150", "aa:bb:cc:dd:ee:ff", "laptop"))

	desired := current.Clone()
	desired.Pools[0].End = addr(t, "192.168.1.210")

	plan := buildPlan(t, desired, obs)

	if plan.Action != ActionRestart {
		t.Errorf("Action = %q, want restart", plan.Action)
	}
	// Growing the range keeps every client, so it is a restart, not a
	// disruption.
	if plan.Impact != ImpactRestart {
		t.Errorf("Impact = %s, want restart; nobody loses an address: %v", plan.Impact, plan.Reasons)
	}
}

// The honest question is not "did a range field change" but "will a client lose
// the address it is using". That is answered from the live lease database.
func TestShrinkingAPoolBelowALiveLeaseIsDisruptive(t *testing.T) {
	current := validConfig(t)
	obs := observe(t, current, true,
		activeLease(t, "192.168.1.110", "aa:bb:cc:dd:ee:ff", "laptop"),
		activeLease(t, "192.168.1.190", "11:22:33:44:55:66", ""),
	)

	desired := current.Clone()
	desired.Pools[0].End = addr(t, "192.168.1.120") // .190 no longer covered

	plan := buildPlan(t, desired, obs)

	if plan.Impact != ImpactDisruptive {
		t.Fatalf("Impact = %s, want disruptive", plan.Impact)
	}
	if len(plan.Reasons) == 0 {
		t.Fatal("a disruptive plan must say who it disrupts")
	}
	reason := strings.Join(plan.Reasons, " ")
	if !strings.Contains(reason, "192.168.1.190") {
		t.Errorf("reason does not name the client that loses its address: %q", reason)
	}
	if strings.Contains(reason, "192.168.1.110") {
		t.Errorf("reason names a client that keeps its address: %q", reason)
	}
}

// A reservation keeps a client served even when the dynamic range no longer
// covers it, because dnsmasq honours a dhcp-host outside the range.
func TestShrinkingAPoolIsNotDisruptiveWhenAReservationCovers(t *testing.T) {
	current := validConfig(t)
	obs := observe(t, current, true, activeLease(t, "192.168.1.190", "aa:bb:cc:dd:ee:ff", "nas"))

	desired := current.Clone()
	desired.Pools[0].End = addr(t, "192.168.1.120")
	desired.SetReservation(Reservation{MAC: "aa:bb:cc:dd:ee:ff", IP: addr(t, "192.168.1.190"), Hostname: "nas"})

	plan := buildPlan(t, desired, obs)

	if plan.Impact == ImpactDisruptive {
		t.Errorf("Impact = disruptive, but the client is pinned by a reservation: %v", plan.Reasons)
	}
}

// An expired lease is not a client to protect.
func TestExpiredLeasesDoNotMakeAChangeDisruptive(t *testing.T) {
	current := validConfig(t)
	expired := Lease{IP: addr(t, "192.168.1.190"), MAC: "aa:bb:cc:dd:ee:ff", Expires: planNow.Add(-time.Hour)}
	obs := observe(t, current, true, expired)

	desired := current.Clone()
	desired.Pools[0].End = addr(t, "192.168.1.120")

	plan := buildPlan(t, desired, obs)

	if plan.Impact == ImpactDisruptive {
		t.Errorf("an expired lease should not count as a dropped client: %v", plan.Reasons)
	}
}

func TestDisablingIsDisruptive(t *testing.T) {
	current := validConfig(t)
	obs := observe(t, current, true, activeLease(t, "192.168.1.110", "aa:bb:cc:dd:ee:ff", "laptop"))

	desired := current.Clone()
	desired.Enabled = false

	plan := buildPlan(t, desired, obs)

	if plan.Action != ActionStop {
		t.Errorf("Action = %q, want stop", plan.Action)
	}
	if plan.Impact != ImpactDisruptive {
		t.Errorf("Impact = %s, want disruptive", plan.Impact)
	}
	if !strings.Contains(strings.Join(plan.Reasons, " "), "no client will be able to renew") {
		t.Errorf("reasons do not explain the stop: %v", plan.Reasons)
	}
}

func TestEnablingStartsTheService(t *testing.T) {
	current := validConfig(t)
	current.Enabled = false
	obs := observe(t, current, false)

	desired := current.Clone()
	desired.Enabled = true

	plan := buildPlan(t, desired, obs)

	if plan.Action != ActionStart {
		t.Errorf("Action = %q, want start", plan.Action)
	}
	if plan.Impact != ImpactRestart {
		t.Errorf("Impact = %s, want restart", plan.Impact)
	}
}

// A stopped daemon that should be running is drift, even with every file
// already correct — design.md §5.4 counts liveness as well as content.
func TestStoppedServiceIsDrift(t *testing.T) {
	c := validConfig(t)
	plan := buildPlan(t, c, observe(t, c, false))

	if plan.Empty() {
		t.Error("a stopped service with an enabled config should not plan as clean")
	}
	if plan.Action != ActionStart {
		t.Errorf("Action = %q, want start", plan.Action)
	}
}

// While the daemon is stopped nothing is disrupted, because nothing is being
// served — writing files is free.
func TestChangesWhileStoppedNeedNoServiceAction(t *testing.T) {
	current := validConfig(t)
	current.Enabled = false
	obs := observe(t, current, false)

	desired := current.Clone()
	desired.Pools[0].End = addr(t, "192.168.1.210")

	plan := buildPlan(t, desired, obs)

	if len(plan.Changes) == 0 {
		t.Fatal("expected the rendered config to change")
	}
	if plan.Action != ActionNone {
		t.Errorf("Action = %q, want none while stopped", plan.Action)
	}
}

// Planning an invalid config must fail before anything is written — that is the
// entire value of validating first (design.md §5.3.1).
func TestPlanRefusesAnInvalidConfig(t *testing.T) {
	c := validConfig(t)
	obs := observe(t, c, true)
	c.Pools[0].Start = addr(t, "10.0.0.5") // outside br-lan's subnet

	plan, err := BuildPlan(NewDnsmasq(DefaultPaths()), c, testLinks(), obs, planNow)
	if err == nil {
		t.Fatal("BuildPlan accepted a config whose pool is outside its interface's subnet")
	}
	if len(plan.Changes) != 0 {
		t.Errorf("a rejected config still produced changes: %v", changePaths(plan))
	}
	if !strings.Contains(err.Error(), "outside every subnet") {
		t.Errorf("error does not explain the problem: %v", err)
	}
}

// Warnings must survive a successful plan, or the operator never sees them.
func TestPlanCarriesWarnings(t *testing.T) {
	c := validConfig(t)
	c.SetReservation(Reservation{MAC: "aa:bb:cc:dd:ee:ff", IP: addr(t, "192.168.1.150")})

	plan := buildPlan(t, c, observe(t, validConfig(t), true))

	if !hasProblem(plan.Validation.Warnings, "reservations[0].ip", "dynamic range") {
		t.Errorf("warning lost: %s", problemStrings(plan.Validation.Warnings))
	}
}

func TestImpactOrdering(t *testing.T) {
	// classify takes the maximum impact across changes, so the constants must
	// stay in increasing order of severity.
	if !(ImpactNone < ImpactReload && ImpactReload < ImpactRestart && ImpactRestart < ImpactDisruptive) {
		t.Fatal("Impact constants are not ordered by severity")
	}
	for impact, want := range map[Impact]string{
		ImpactNone: "none", ImpactReload: "reload",
		ImpactRestart: "restart", ImpactDisruptive: "disruptive",
	} {
		if got := impact.String(); got != want {
			t.Errorf("Impact.String() = %q, want %q", got, want)
		}
	}
}

// `disruptive` is computed from the live lease database rather than by
// comparing config fields, which is what makes it a fact (design.md §11.3). The
// question is "does this client keep the address it is holding", and a
// reservation is the case where the config looks additive but the client moves.
func TestPinningAClientToADifferentAddressIsDisruptive(t *testing.T) {
	current := validConfig(t)
	obs := observe(t, current, true, activeLease(t, "192.168.1.150", "aa:bb:cc:dd:ee:ff", "laptop"))

	desired := current.Clone()
	desired.SetReservation(Reservation{MAC: "aa:bb:cc:dd:ee:ff", IP: addr(t, "192.168.1.50")})

	plan := buildPlan(t, desired, obs)

	if plan.Impact != ImpactDisruptive {
		t.Errorf("Impact = %s, want disruptive: the client is pinned away from the address it holds", plan.Impact)
	}
	if len(plan.Reasons) == 0 || !strings.Contains(plan.Reasons[0], "laptop") {
		t.Errorf("reasons do not name the client that loses its address: %v", plan.Reasons)
	}
}

// The inverse, which matters just as much: over-reporting `disruptive` teaches
// operators to click through it.
func TestPinningAClientToTheAddressItHoldsIsNotDisruptive(t *testing.T) {
	current := validConfig(t)
	obs := observe(t, current, true, activeLease(t, "192.168.1.150", "aa:bb:cc:dd:ee:ff", "laptop"))

	desired := current.Clone()
	desired.SetReservation(Reservation{MAC: "aa:bb:cc:dd:ee:ff", IP: addr(t, "192.168.1.150")})

	plan := buildPlan(t, desired, obs)

	if plan.Impact != ImpactReload {
		t.Errorf("Impact = %s, want reload: the client keeps its address", plan.Impact)
	}
}

func TestReservingAnAddressAnotherClientHoldsIsDisruptive(t *testing.T) {
	current := validConfig(t)
	obs := observe(t, current, true, activeLease(t, "192.168.1.150", "aa:bb:cc:dd:ee:ff", "laptop"))

	desired := current.Clone()
	desired.SetReservation(Reservation{MAC: "11:22:33:44:55:66", IP: addr(t, "192.168.1.150")})

	plan := buildPlan(t, desired, obs)

	if plan.Impact != ImpactDisruptive {
		t.Errorf("Impact = %s, want disruptive: the address is promised to somebody else", plan.Impact)
	}
}

// Pools and reservations are IPv4, and dnsmasq's lease file does not record the
// interface a lease was handed out on — so an IPv6 lease matches no range by
// construction. Counting those as dropped would mark every apply on an
// IPv6-enabled box disruptive, which is the fastest way to make the word
// meaningless.
func TestIPv6LeasesDoNotMakeEveryChangeDisruptive(t *testing.T) {
	current := validConfig(t)
	current.Pools[0].RA = RASLAAC
	v6 := Lease{IP: addr(t, "2001:db8::1234"), IAID: "1", Expires: planNow.Add(time.Hour)}
	obs := observe(t, current, true,
		activeLease(t, "192.168.1.150", "aa:bb:cc:dd:ee:ff", "laptop"), v6)

	desired := current.Clone()
	desired.Pools[0].Domain = "lan"

	plan := buildPlan(t, desired, obs)

	if plan.Impact == ImpactDisruptive {
		t.Errorf("setting a domain was called disruptive because of an IPv6 lease: %v", plan.Reasons)
	}
}

// Turning DHCP off is the one case where an IPv6 lease is genuinely lost too:
// nothing renews.
func TestDisablingDropsEveryLeaseIncludingIPv6(t *testing.T) {
	current := validConfig(t)
	current.Pools[0].RA = RASLAAC
	v6 := Lease{IP: addr(t, "2001:db8::1234"), IAID: "1", Expires: planNow.Add(time.Hour)}
	obs := observe(t, current, true,
		activeLease(t, "192.168.1.150", "aa:bb:cc:dd:ee:ff", "laptop"), v6)

	desired := current.Clone()
	desired.Enabled = false

	plan := buildPlan(t, desired, obs)

	if plan.Impact != ImpactDisruptive {
		t.Errorf("Impact = %s, want disruptive", plan.Impact)
	}
	if dropped := Dropped(desired, obs.Leases, planNow); len(dropped) != 2 {
		t.Errorf("dropped %d leases, want both", len(dropped))
	}
}

func changePaths(p Plan) []string {
	out := make([]string, len(p.Changes))
	for i, c := range p.Changes {
		out[i] = string(c.Kind) + " " + c.Path
	}
	return out
}
