package firewall

import (
	"context"
	"strings"
	"testing"
)

// planAgainst builds a plan for cfg against a kernel holding `state`.
func planAgainst(t *testing.T, cfg Config, k *StaticKernel) Plan {
	t.Helper()
	cfg.Normalize()
	obs, err := k.Observe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	plan, _, err := BuildPlan(cfg, testLinks(), obs)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

// applied returns a kernel already holding what cfg asks for.
func applied(t *testing.T, cfg Config) *StaticKernel {
	t.Helper()
	cfg.Normalize()
	k := &StaticKernel{}
	if _, err := k.Apply(context.Background(), Render(cfg, testLinks())); err != nil {
		t.Fatal(err)
	}
	return k
}

// design.md §5.4: drift is not separate machinery, it is the plan against
// unchanged intent.
func TestPlanAgainstAnAppliedKernelIsEmpty(t *testing.T) {
	cfg := testConfig()
	plan := planAgainst(t, cfg, applied(t, cfg))

	if !plan.Empty() {
		t.Errorf("an already-applied config still wants to change %d things: %+v",
			len(plan.Changes), plan.Changes)
	}
	if plan.Impact != ImpactNone {
		t.Errorf("impact = %v, want none", plan.Impact)
	}
}

func TestPlanOnAnUnknownKernelReportsNothingRatherThanEverything(t *testing.T) {
	// A machine that is not a router, or one without CAP_NET_ADMIN. Reporting
	// "everything must be created" would be a plan nobody can act on, and "no
	// change" would be a lie.
	plan := planAgainst(t, testConfig(), &StaticKernel{Unknown: true})
	if !plan.Empty() {
		t.Errorf("an unreadable kernel produced changes: %+v", plan.Changes)
	}
}

// Adding a forward cannot move a connection that is already established: DNAT
// applies to a connection's first packet and conntrack carries the translation
// for the rest.
func TestAddingAForwardIsReload(t *testing.T) {
	before := testConfig()
	k := applied(t, before)

	after := before
	after.Forwards = append(append([]Forward(nil), before.Forwards...), Forward{
		Name: "ssh-nas", In: "wan0", Port: SinglePort(2222),
		To: mustAddrPort("192.168.1.20:22"),
	})

	plan := planAgainst(t, after, k)
	if plan.Empty() {
		t.Fatal("adding a forward changed nothing")
	}
	if plan.Impact != ImpactReload {
		t.Errorf("impact = %v, want reload: %v", plan.Impact, plan.Reasons)
	}
}

// Removing one is the opposite: the conntrack entry only exists while the rule
// does, so established connections stop being translated and die mid-stream.
func TestRemovingAUsedForwardIsDisruptive(t *testing.T) {
	cfg := testConfig()
	k := applied(t, cfg)
	k.Counters = map[string]Counter{"fwd1": {Packets: 42, Bytes: 4096}}

	empty := Config{Enabled: true}
	plan := planAgainst(t, empty, k)

	if plan.Impact != ImpactDisruptive {
		t.Fatalf("impact = %v, want disruptive: %v", plan.Impact, plan.Reasons)
	}
	if !strings.Contains(strings.Join(plan.Reasons, " "), `"web"`) {
		t.Errorf("the reason does not name the forward being removed: %v", plan.Reasons)
	}
}

// §5.3.3 insists `disruptive` be a fact rather than a guess: a classification
// that cried wolf would train the operator to click through the one dialog that
// matters. A counter that has never moved is proof that no connection depends on
// the rule.
func TestRemovingAnUnusedForwardIsNotDisruptive(t *testing.T) {
	cfg := testConfig()
	k := applied(t, cfg)
	k.Counters = map[string]Counter{"fwd1": {Packets: 0}}

	plan := planAgainst(t, Config{Enabled: true}, k)

	if plan.Impact == ImpactDisruptive {
		t.Errorf("removing a forward nothing ever used was called disruptive: %v", plan.Reasons)
	}
	if !strings.Contains(strings.Join(plan.Reasons, " "), "nothing has ever arrived") {
		t.Errorf("the plan does not say why it is safe: %v", plan.Reasons)
	}
}

// When we cannot read the counters at all we must not claim the rule is idle.
// One extra confirmation prompt beats a silently broken connection.
func TestRemovingWithNoCounterReadIsDisruptive(t *testing.T) {
	cfg := testConfig()
	k := applied(t, cfg) // no Counters set at all

	if plan := planAgainst(t, Config{Enabled: true}, k); plan.Impact != ImpactDisruptive {
		t.Errorf("impact = %v with no counter to judge by, want disruptive", plan.Impact)
	}
}

func TestDisablingSaysWhatItMeans(t *testing.T) {
	cfg := testConfig()
	k := applied(t, cfg)
	k.Counters = map[string]Counter{"fwd1": {Packets: 1}}

	off := cfg
	off.Enabled = false
	plan := planAgainst(t, off, k)

	if !strings.Contains(strings.Join(plan.Reasons, " "), "nothing from outside will reach") {
		t.Errorf("disabling does not say what it does: %v", plan.Reasons)
	}
}

// docs/firewall.md §5.2: this is not something we can fix, so it is reported and
// never blocks. The phrasing has to stay hedged — the foreign chain may have an
// accept rule for exactly this traffic that we cannot evaluate.
func TestForeignForwardPolicyIsReportedAndDoesNotBlock(t *testing.T) {
	cfg := testConfig()
	k := &StaticKernel{Foreign: []ForeignFilter{
		{Table: "firewalld", Family: "inet", Chain: "filter_FORWARD", Policy: "drop"},
	}}

	plan := planAgainst(t, cfg, k)

	if len(plan.Foreign) != 1 {
		t.Fatalf("the foreign filter was not carried on the plan: %+v", plan.Foreign)
	}
	joined := strings.Join(plan.Reasons, " ")
	if !strings.Contains(joined, "firewalld") {
		t.Errorf("the reason does not say where to look: %v", plan.Reasons)
	}
	if !strings.Contains(joined, "may be stopped") {
		t.Errorf("the reason overstates what we know: %v", plan.Reasons)
	}
	// Unlike internal/gateway's foreign ip rules, this never refuses.
	if _, _, err := BuildPlan(cfg, testLinks(), mustObserve(t, k)); err != nil {
		t.Errorf("a foreign forward policy blocked the plan: %v", err)
	}
}

// Tearing our own state down cannot conflict with anybody, so saying so then
// would be noise attached to the one operation it cannot affect.
func TestForeignFilterIsNotReportedWhenDisabling(t *testing.T) {
	cfg := testConfig()
	cfg.Enabled = false
	k := &StaticKernel{Foreign: []ForeignFilter{
		{Table: "firewalld", Family: "inet", Chain: "filter_FORWARD", Policy: "drop"},
	}}

	if plan := planAgainst(t, cfg, k); len(plan.Foreign) != 0 {
		t.Errorf("reported a foreign filter while removing our own rules: %+v", plan.Foreign)
	}
}

// docs/firewall.md §5.2's second row, and the one that costs the operator their
// session.
func TestForwardingAPortThisBoxServesWarns(t *testing.T) {
	cfg := testConfig()
	cfg.Forwards[0].Port = SinglePort(22)
	cfg.Forwards[0].To = mustAddrPort("192.168.1.20:22")

	k := &StaticKernel{Listening: []ListeningPort{{Protocol: ProtocolTCP, Port: 22}}}
	plan := planAgainst(t, cfg, k)

	joined := strings.Join(plan.Reasons, " ")
	if !strings.Contains(joined, "tcp/22") {
		t.Errorf("no warning about forwarding a port this box serves: %v", plan.Reasons)
	}
	if !strings.Contains(joined, "including, if") {
		t.Errorf("the warning does not say the operator's own session is at stake: %v", plan.Reasons)
	}
}

// The conflict check must not fire on a protocol the forward does not carry.
func TestPortConflictRespectsTheProtocol(t *testing.T) {
	cfg := testConfig() // tcp/8080
	k := &StaticKernel{Listening: []ListeningPort{{Protocol: ProtocolUDP, Port: 8080}}}

	if plan := planAgainst(t, cfg, k); strings.Contains(strings.Join(plan.Reasons, " "), "listening") {
		t.Errorf("warned about a udp listener for a tcp forward: %v", plan.Reasons)
	}
}

// A range forward has to notice a listener anywhere inside it.
func TestPortConflictCoversARange(t *testing.T) {
	cfg := testConfig()
	cfg.Forwards[0].Port = PortRange{30000, 30010}
	cfg.Forwards[0].To = mustAddrPort("192.168.1.10:30000")

	k := &StaticKernel{Listening: []ListeningPort{{Protocol: ProtocolTCP, Port: 30005}}}
	if plan := planAgainst(t, cfg, k); !strings.Contains(strings.Join(plan.Reasons, " "), "tcp/30005") {
		t.Errorf("a listener inside the forwarded range went unnoticed: %v", plan.Reasons)
	}
}

// A hand-run `nft delete rule` has to show up the same way a hand-edited config
// file does in the other modules.
func TestAHandRemovedRuleShowsAsDrift(t *testing.T) {
	cfg := testConfig()
	k := applied(t, cfg)

	var kept []string
	for _, l := range k.State {
		if !strings.HasPrefix(l, "nft dnat ") {
			kept = append(kept, l)
		}
	}
	k.State = kept

	plan := planAgainst(t, cfg, k)
	if plan.Empty() {
		t.Fatal("a rule deleted by hand was not noticed")
	}
	found := false
	for _, c := range plan.Changes {
		if c.Kind == ChangeAdd && strings.HasPrefix(c.Line, "nft dnat ") {
			found = true
		}
	}
	if !found {
		t.Errorf("the plan does not put the rule back: %+v", plan.Changes)
	}
}

func TestImpactRoundTripsAsText(t *testing.T) {
	// Impact is an int with a text encoding, so without the pair a plan could be
	// sent and never read — and `olr firewall` is a client of its own API.
	for _, want := range []Impact{ImpactNone, ImpactReload, ImpactRestart, ImpactDisruptive} {
		text, err := want.MarshalText()
		if err != nil {
			t.Fatal(err)
		}
		var got Impact
		if err := got.UnmarshalText(text); err != nil || got != want {
			t.Errorf("%v round-tripped to %v (%v)", want, got, err)
		}
	}
	var bad Impact
	if err := bad.UnmarshalText([]byte("sideways")); err == nil {
		t.Error("an unknown impact was accepted")
	}
}

func mustObserve(t *testing.T, k *StaticKernel) Observed {
	t.Helper()
	obs, err := k.Observe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return obs
}
