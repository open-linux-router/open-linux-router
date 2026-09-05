package routing

import (
	"context"
	"net/netip"
	"testing"
)

func markOf(t *testing.T, c Config, name string) uint32 {
	t.Helper()
	e, ok := c.Find(name)
	if !ok {
		t.Fatalf("no exit named %q", name)
	}
	return e.Mark()
}

// The join this endpoint exists for: a mark is a number the operator never
// chose and cannot look up, so the API answers with the name they gave it
// (design.md §4.5 — the model and the query interface are ours).
func TestTrafficNamesTheExitBehindEachMark(t *testing.T) {
	c := testConfig()
	c.Normalize()

	k := &StaticKernel{Flows: []Flow{
		{Addr: netip.MustParseAddr("192.168.1.23"), Mark: markOf(t, c, "Clash"),
			UpBytes: 100, DownBytes: 900},
	}}
	a := Applier{Kernel: k, Links: testLinks(), Store: newTestStore(t)}
	if _, err := a.Save(c); err != nil {
		t.Fatal(err)
	}

	usage, err := a.Traffic(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(usage) != 1 {
		t.Fatalf("expected one row, got %+v", usage)
	}
	if usage[0].Exit != "Clash" {
		t.Errorf("mark was not resolved to a name: %+v", usage[0])
	}
	if usage[0].Total() != 1000 {
		t.Errorf("total = %d, want 1000", usage[0].Total())
	}
}

// §7.3: show what you cannot account for. Traffic that matched no assignment
// carries mark 0, and it is a row rather than an omission — per-exit totals
// only reconcile against the box total if the residual is visible.
func TestTrafficKeepsTheResidualAsARow(t *testing.T) {
	c := testConfig()
	c.Normalize()

	k := &StaticKernel{Flows: []Flow{
		{Addr: netip.MustParseAddr("192.168.9.9"), Mark: 0, UpBytes: 5, DownBytes: 5},
	}}
	a := Applier{Kernel: k, Links: testLinks(), Store: newTestStore(t)}
	if _, err := a.Save(c); err != nil {
		t.Fatal(err)
	}

	usage, err := a.Traffic(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(usage) != 1 {
		t.Fatalf("the residual should survive, got %+v", usage)
	}
	if usage[0].Exit != "" || usage[0].Unknown {
		t.Errorf("unpoliced traffic is not unknown, it is unpoliced: %+v", usage[0])
	}
}

// A mark naming a slot no exit holds any more — which happens for as long as
// flows outlive a deleted exit. Reported as unknown rather than silently
// folded into the residual, because "went somewhere that no longer exists" and
// "matched no rule" are different facts.
func TestTrafficMarksADeletedExitsFlowsUnknown(t *testing.T) {
	c := testConfig()
	c.Normalize()

	k := &StaticKernel{Flows: []Flow{
		{Addr: netip.MustParseAddr("192.168.1.5"), Mark: Slot(MaxSlot).Mark(), UpBytes: 1},
	}}
	a := Applier{Kernel: k, Links: testLinks(), Store: newTestStore(t)}
	if _, err := a.Save(c); err != nil {
		t.Fatal(err)
	}

	usage, err := a.Traffic(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(usage) != 1 || !usage[0].Unknown {
		t.Fatalf("expected one unknown row, got %+v", usage)
	}
}

func TestTrafficSortsHeaviestFirst(t *testing.T) {
	c := testConfig()
	c.Normalize()

	k := &StaticKernel{Flows: []Flow{
		{Addr: netip.MustParseAddr("192.168.1.2"), UpBytes: 1},
		{Addr: netip.MustParseAddr("192.168.1.3"), UpBytes: 1000},
		{Addr: netip.MustParseAddr("192.168.1.4"), DownBytes: 50},
	}}
	a := Applier{Kernel: k, Links: testLinks(), Store: newTestStore(t)}
	if _, err := a.Save(c); err != nil {
		t.Fatal(err)
	}

	usage, err := a.Traffic(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// The question the screen answers is "who is using the bandwidth", so the
	// answer belongs at the top.
	for i := 1; i < len(usage); i++ {
		if usage[i-1].Total() < usage[i].Total() {
			t.Fatalf("not sorted heaviest first: %+v", usage)
		}
	}
}

func TestTrafficOnAKernelThatCannotAnswer(t *testing.T) {
	c := testConfig()
	c.Normalize()

	a := Applier{Kernel: &StaticKernel{Unknown: true}, Links: testLinks(), Store: newTestStore(t)}
	if _, err := a.Save(c); err != nil {
		t.Fatal(err)
	}

	// An error rather than an empty list, because "nothing used the network" is
	// a claim and this is the absence of one. The HTTP layer turns it into
	// counting:false, which is the honest thing to put on a screen.
	if _, err := a.Traffic(context.Background()); err == nil {
		t.Fatal("expected an error from a kernel that cannot be read")
	}
}

// §7.4's limits ride on the response rather than living in the SPA, so the CLI
// and any agent reading this endpoint get the same caveats. Every one of them
// explains a number being smaller than expected.
func TestTrafficViewCarriesItsLimits(t *testing.T) {
	c := testConfig()
	v := viewTraffic(c, nil, true, testTime())
	if len(v.Limits) == 0 {
		t.Fatal("a counting response should state what it cannot see")
	}
	if !v.Counting || !v.Enabled {
		t.Errorf("unexpected flags: %+v", v)
	}

	// Nothing is being counted, so there is nothing to caveat.
	off := viewTraffic(c, nil, false, testTime())
	if len(off.Limits) != 0 {
		t.Errorf("limits should not be stated when nothing is counted: %+v", off.Limits)
	}
	if off.Counting {
		t.Error("counting should be false")
	}
}

// §7.3 pointed at ourselves: a full accounting table goes on counting the
// devices it already holds and silently records no new ones, so "this device
// uses no data" and "this device is missing" look identical on screen unless
// the response says how full it is.
func TestTrafficViewSaysWhenItIsRunningOutOfRoom(t *testing.T) {
	c := testConfig()

	roomy := viewTraffic(c, []Usage{{Addr: netip.MustParseAddr("192.168.1.5")}}, true, testTime())
	if roomy.Capacity != StatSetSize {
		t.Errorf("capacity = %d, want %d", roomy.Capacity, StatSetSize)
	}
	if roomy.Held != 1 {
		t.Errorf("held = %d, want 1", roomy.Held)
	}
	if roomy.Saturated() {
		t.Error("one device should not read as a full table")
	}

	full := make([]Usage, StatSetSize*9/10)
	for i := range full {
		full[i] = Usage{Addr: netip.MustParseAddr("192.168.1.5")}
	}
	if !viewTraffic(c, full, true, testTime()).Saturated() {
		t.Error("a table at nine tenths should say so while the numbers are still mostly right")
	}
}
