package routing

import (
	"context"
	"net/netip"
	"strings"
	"testing"
)

// programmed returns a kernel already holding exactly what c asks for, which is
// the starting point for every "does an edit produce the right plan?" test.
func programmed(t *testing.T, c Config) *StaticKernel {
	t.Helper()
	c.Normalize()
	d := Render(c, testLinks(), nil)
	k := &StaticKernel{State: d.objectLines(), Sysctls: map[string]string{}}
	for _, s := range d.Sysctls {
		k.Sysctls[s.Key] = s.Value
	}
	return k
}

func planFor(t *testing.T, c Config, k *StaticKernel, admin netip.Addr) Plan {
	t.Helper()
	c.Normalize()
	obs, err := k.Observe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	plan, _, err := BuildPlan(c, testLinks(), nil, obs, admin)
	if err != nil {
		t.Fatalf("building the plan: %v", err)
	}
	return plan
}

func changeLines(p Plan) []string {
	out := make([]string, 0, len(p.Changes))
	for _, c := range p.Changes {
		out = append(out, string(c.Kind)+" "+c.Line)
	}
	return out
}

// design.md §5.4: drift is not separate machinery, it is the plan against
// unchanged intent coming back empty.
func TestAnAlreadyProgrammedKernelPlansEmpty(t *testing.T) {
	c := testConfig()
	plan := planFor(t, c, programmed(t, c), netip.Addr{})

	if !plan.Empty() {
		t.Fatalf("expected no drift, got %v", changeLines(plan))
	}
	if plan.Impact != ImpactNone {
		t.Errorf("impact = %s, want none", plan.Impact)
	}
}

func TestAHandRemovedRuleShowsAsDrift(t *testing.T) {
	c := testConfig()
	k := programmed(t, c)

	// Somebody ran `ip rule del`. On the other modules that would be a
	// hand-edited file; here the kernel is the file.
	var kept []string
	for _, l := range k.State {
		if !strings.HasPrefix(l, "rule ip priority 8100 ") {
			kept = append(kept, l)
		}
	}
	k.State = kept

	plan := planFor(t, c, k, netip.Addr{})
	if plan.Empty() {
		t.Fatal("a missing ip rule should be drift")
	}
	if got := changeLines(plan); len(got) != 1 || !strings.HasPrefix(got[0], "add rule ip priority 8100") {
		t.Errorf("expected the local guard to be re-added, got %v", got)
	}
}

// §3.4: an assignment edit only changes classify rules, and `ct mark` restore
// means flows already running keep the exit they started on. That is the whole
// reason the save/restore pair is worth two rules.
func TestChangingAnAssignmentIsAReloadNotADisruption(t *testing.T) {
	before := testConfig()
	k := programmed(t, before)

	after := testConfig()
	after.Interfaces[0].Exit = "Blocked"

	plan := planFor(t, after, k, netip.Addr{})
	if plan.Empty() {
		t.Fatal("the classify rules should have changed")
	}
	if plan.Impact != ImpactReload {
		t.Errorf("impact = %s, want reload; changes were %v", plan.Impact, changeLines(plan))
	}
}

// §5.3.3: `disruptive` is a fact, so it needs somebody actually using the path
// that moves.
func TestMovingAPathNobodyIsUsingIsNotDisruptive(t *testing.T) {
	before := testConfig()
	k := programmed(t, before)
	k.Active = nil

	after := testConfig()
	setExit(&after, "Clash", func(e *Exit) { e.Via.NextHop = hop("192.168.1.51") })

	plan := planFor(t, after, k, netip.Addr{})
	if plan.Impact == ImpactDisruptive {
		t.Errorf("nothing is on the network, so nothing can be disconnected: %v", plan.Reasons)
	}
}

func TestMovingAPathSomebodyIsUsingIsDisruptive(t *testing.T) {
	before := testConfig()
	k := programmed(t, before)
	k.Active = []string{"192.168.1.23"}

	after := testConfig()
	setExit(&after, "Clash", func(e *Exit) { e.Via.NextHop = hop("192.168.1.51") })

	plan := planFor(t, after, k, netip.Addr{})
	if plan.Impact != ImpactDisruptive {
		t.Fatalf("impact = %s, want disruptive", plan.Impact)
	}
	if !reasonsContain(plan, "192.168.1.23") {
		t.Errorf("the reason should name who is affected, got %v", plan.Reasons)
	}
}

// §5.1's last row: the one mistake this module can cause that the operator
// cannot undo by clicking again.
func TestRoutingTheAdminsOwnAddressIsDisruptive(t *testing.T) {
	before := Config{Enabled: true}
	k := programmed(t, before)

	after := testConfig()
	admin := netip.MustParseAddr("192.168.1.99")

	plan := planFor(t, after, k, admin)
	if plan.Impact != ImpactDisruptive {
		t.Fatalf("impact = %s, want disruptive", plan.Impact)
	}
	if !reasonsContain(plan, "disconnect you") {
		t.Errorf("the reason should say so plainly, got %v", plan.Reasons)
	}
}

func TestAnAdminOnAnUnaffectedNetworkIsNotWarned(t *testing.T) {
	before := Config{Enabled: true}
	k := programmed(t, before)

	plan := planFor(t, testConfig(), k, netip.MustParseAddr("10.9.9.9"))
	if reasonsContain(plan, "disconnect you") {
		t.Errorf("an address on no assigned network is not at risk: %v", plan.Reasons)
	}
}

// §6: structural detection, and refusal rather than racing another owner of the
// routing table.
func TestForeignRulesBlockThePlan(t *testing.T) {
	c := testConfig()
	k := programmed(t, c)
	k.Foreign = []ForeignRule{
		{Priority: 9000, Family: "ip", Table: 2022, Selector: "from all lookup 2022", HasDefault: true},
	}

	plan := planFor(t, c, k, netip.Addr{})
	if plan.Blocked == "" {
		t.Fatal("a foreign rule owning a default route should block the plan")
	}
	for _, want := range []string{"9000", "2022", "auto-route"} {
		if !strings.Contains(plan.Blocked, want) {
			t.Errorf("the refusal should mention %q, got %q", want, plan.Blocked)
		}
	}
}

// Tearing our own state down never conflicts with anybody, and refusing to do
// so would leave an operator unable to back out of the situation the refusal is
// about.
func TestForeignRulesDoNotBlockATeardown(t *testing.T) {
	c := testConfig()
	k := programmed(t, c)
	k.Foreign = []ForeignRule{{Priority: 9000, Table: 2022, HasDefault: true}}

	off := testConfig()
	off.Enabled = false

	plan := planFor(t, off, k, netip.Addr{})
	if plan.Blocked != "" {
		t.Fatalf("disabling should always be possible, got %q", plan.Blocked)
	}
}

func TestDisablingRemovesEverything(t *testing.T) {
	c := testConfig()
	k := programmed(t, c)

	off := testConfig()
	off.Enabled = false

	plan := planFor(t, off, k, netip.Addr{})
	for _, ch := range plan.Changes {
		if ch.Kind != ChangeRemove {
			t.Errorf("disabling should only remove, got %s %q", ch.Kind, ch.Line)
		}
	}
	if !reasonsContain(plan, "box's normal path") {
		t.Errorf("the operator should be told what disabling means, got %v", plan.Reasons)
	}
}

// A kernel we could not read is a different answer from a kernel with nothing
// in it, and conflating them would make every developer machine report drift.
func TestAnUnreadableKernelPlansNothing(t *testing.T) {
	c := testConfig()
	k := &StaticKernel{Unknown: true}

	plan := planFor(t, c, k, netip.Addr{})
	if !plan.Empty() {
		t.Errorf("nothing can be planned against an unknown kernel, got %v", changeLines(plan))
	}
}

// The sysctls are the one namespace we edit rather than own, so they move in
// one direction only.
func TestASysctlAlreadyAtZeroIsNotAChange(t *testing.T) {
	c := testConfig()
	c.Normalize()
	desired := Render(c, testLinks(), nil)

	k := &StaticKernel{State: desired.objectLines()}
	obs, _ := k.Observe(context.Background())
	obs.Sysctls = map[string]string{}
	for _, s := range desired.Sysctls {
		obs.Sysctls[s.Key] = s.Value
	}

	plan, _, err := BuildPlan(c, testLinks(), nil, obs, netip.Addr{})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Empty() {
		t.Errorf("sysctls already correct should produce no work, got %v", changeLines(plan))
	}
}

func TestASysctlSomebodyElseSetIsNeverRemoved(t *testing.T) {
	// No exits at all, so we want no sysctls — but the box has some at 0
	// already. Those belong to whoever set them.
	c := Config{Enabled: true}
	c.Normalize()

	obs := Observed{
		Known: true,
		Sysctls: map[string]string{
			"net.ipv4.conf.eth9.send_redirects": "0",
		},
	}
	plan, _, err := BuildPlan(c, testLinks(), nil, obs, netip.Addr{})
	if err != nil {
		t.Fatal(err)
	}
	for _, ch := range plan.Changes {
		if strings.Contains(ch.Line, "eth9") {
			t.Errorf("we must not touch a sysctl we did not ask for: %s %q", ch.Kind, ch.Line)
		}
	}
}

func TestAllSendRedirectsIsReportedNotWritten(t *testing.T) {
	c := testConfig()
	c.Normalize()
	k := &StaticKernel{}
	on := true
	k.AllSendRedirects = &on

	plan := planFor(t, c, k, netip.Addr{})
	if !reasonsContain(plan, "net.ipv4.conf.all.send_redirects") {
		t.Fatalf("the machine-wide sysctl should be reported, got %v", plan.Reasons)
	}
	for _, ch := range plan.Changes {
		if strings.Contains(ch.Line, "conf.all.") {
			t.Errorf("olr must not write machine-wide sysctls: %q", ch.Line)
		}
	}
}

func TestImpactRoundTripsThroughJSON(t *testing.T) {
	for _, want := range []Impact{ImpactNone, ImpactReload, ImpactRestart, ImpactDisruptive} {
		text, err := want.MarshalText()
		if err != nil {
			t.Fatal(err)
		}
		var got Impact
		if err := got.UnmarshalText(text); err != nil {
			t.Fatalf("%q: %v", text, err)
		}
		if got != want {
			t.Errorf("%q decoded as %s", text, got)
		}
	}
}

func TestApplyStoresIntentEvenWhenTheKernelRefuses(t *testing.T) {
	// A config that cannot be applied right now is still the config the
	// operator asked for; losing it on the way to reporting the problem would
	// make the problem worse.
	c := testConfig()
	c.Normalize()

	store := newTestStore(t)
	a := Applier{
		Kernel: &StaticKernel{ApplyErr: errBoom, FailAfter: 1},
		Links:  testLinks(),
		Store:  store,
	}

	result, stored, err := a.Apply(context.Background(), c, netip.Addr{})
	if err == nil {
		t.Fatal("expected the kernel failure to surface")
	}
	if len(stored.Exits) != len(c.Exits) {
		t.Errorf("intent was not stored: %+v", stored)
	}
	// §5.3.2: no rollback, so which steps landed is the operator's starting
	// point and has to be reported.
	if len(result.Steps) == 0 {
		t.Error("the steps that landed should be reported")
	}
	reloaded, err := a.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Exits) != len(c.Exits) {
		t.Errorf("intent did not reach the document: %+v", reloaded)
	}
}

func reasonsContain(p Plan, substr string) bool {
	for _, r := range p.Reasons {
		if strings.Contains(r, substr) {
			return true
		}
	}
	return strings.Contains(p.Blocked, substr)
}
