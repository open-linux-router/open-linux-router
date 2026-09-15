package core

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- a systemd this box does not have --------------------------------------

// fakeUnits records what was done to it, which is the whole point: the bug this
// feature could introduce is standing down a unit nobody asked about, and that
// is invisible unless the calls are counted.
type fakeUnits struct {
	status map[string]UnitStatus
	acted  []string
	fail   map[string]error
}

type fakeUnit struct {
	all  *fakeUnits
	name string
}

func (u fakeUnit) Status(context.Context) (UnitStatus, error) {
	st := u.all.status[u.name]
	st.Unit = u.name
	return st, nil
}

func (u fakeUnit) did(verb string) error {
	u.all.acted = append(u.all.acted, verb+" "+u.name)
	if err := u.all.fail[verb+" "+u.name]; err != nil {
		return err
	}
	// Reflected back into status so a second read sees the box as it now is,
	// which is what standDownShadows re-reads.
	st := u.all.status[u.name]
	switch verb {
	case "stop":
		st.Active = false
	case "disable":
		st.Enabled = false
	}
	u.all.status[u.name] = st
	return nil
}

func (u fakeUnit) Start(context.Context) error   { return u.did("start") }
func (u fakeUnit) Stop(context.Context) error    { return u.did("stop") }
func (u fakeUnit) Restart(context.Context) error { return u.did("restart") }
func (u fakeUnit) Reload(context.Context) error  { return u.did("reload") }
func (u fakeUnit) Enable(context.Context) error  { return u.did("enable") }
func (u fakeUnit) Disable(context.Context) error { return u.did("disable") }

func withFakeUnits(t *testing.T, status map[string]UnitStatus) *fakeUnits {
	t.Helper()
	f := &fakeUnits{status: status, fail: map[string]error{}}
	prev := newUnit
	t.Cleanup(func() { newUnit = prev })
	newUnit = func(name string) (Unit, error) { return fakeUnit{all: f, name: name}, nil }
	return f
}

// withFakePackageManager stands in for apt. Nothing in this package may ever
// run a real one under test, so the seam is checked as well as used: a test
// that forgot to swap it would install something on the machine running `go
// test`.
func withFakePackageManager(t *testing.T, err error) *[][]string {
	t.Helper()
	var ran [][]string

	prevRun, prevUID := runPrivileged, geteuid
	t.Cleanup(func() { runPrivileged, geteuid = prevRun, prevUID })

	geteuid = func() int { return 0 }
	runPrivileged = func(_ context.Context, argv []string) ([]byte, error) {
		ran = append(ran, argv)
		return []byte("fake package manager output"), err
	}
	return &ran
}

// --- the tables ------------------------------------------------------------

// The advice and the action come from two tables, and the failure they can
// produce together is the worst one available: olr telling an operator it will
// run one command and running another. They cover the same families or neither
// is trustworthy.
func TestInstallAdviceAndInstallActionCoverTheSameFamilies(t *testing.T) {
	for family := range installCommands {
		if _, ok := installArgv[family]; !ok {
			t.Errorf("%s is advised a command but olr cannot run one for it", family)
		}
	}
	for family := range installArgv {
		if _, ok := installCommands[family]; !ok {
			t.Errorf("%s can be installed by olr but is never advised, so nothing reports it", family)
		}
	}
	// installOrder decides which family wins for an ID_LIKE naming several. A
	// family missing from it is unreachable through both tables at once.
	inOrder := map[string]bool{}
	for _, f := range installOrder {
		inOrder[f] = true
	}
	for family := range installArgv {
		if !inOrder[family] {
			t.Errorf("%s is in installArgv but not installOrder, so it is never selected", family)
		}
	}
}

// Each family's own way of saying "assume yes", spelled out here rather than
// asserted generically, so that editing the table without thinking fails.
func TestInstallArgvIsScriptable(t *testing.T) {
	want := map[string][]string{
		// apt-get, never apt: `apt` prints "does not have a stable CLI
		// interface" the moment it is scripted, and its output format is
		// explicitly not a contract.
		"debian": {"apt-get", "install", "-y", "-o", "DPkg::Lock::Timeout=60"},
		"fedora": {"dnf", "install", "-y"},
		"rhel":   {"dnf", "install", "-y"},
		"arch":   {"pacman", "-S", "--noconfirm"},
		// apk is non-interactive already; a flag invented for symmetry would
		// fail every install on Alpine.
		"alpine": {"apk", "add"},
		"suse":   {"zypper", "--non-interactive", "install"},
	}
	for family, expected := range want {
		got, ok := installArgv[family]
		if !ok {
			t.Errorf("%s has no install argv", family)
			continue
		}
		if strings.Join(got, " ") != strings.Join(expected, " ") {
			t.Errorf("%s installs with %q; want %q", family, got, expected)
		}
	}
	if len(installArgv) != len(want) {
		t.Errorf("installArgv has %d families and this test knows %d", len(installArgv), len(want))
	}
}

func TestInstallArgvFollowsTheDistro(t *testing.T) {
	for _, tc := range []struct {
		name      string
		osRelease string
		want      string
	}{
		{"debian", debianOSRelease, "apt-get install -y -o DPkg::Lock::Timeout=60 unbound"},
		{"ubuntu via ID_LIKE", ubuntuOSRelease, "apt-get install -y -o DPkg::Lock::Timeout=60 unbound"},
		{"fedora", fedoraOSRelease, "dnf install -y unbound"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withOSRelease(t, tc.osRelease)
			argv, ok := installArgvFor(DetectDistro(), "unbound")
			if !ok {
				t.Fatal("no argv for a distribution we advise a command for")
			}
			if got := strings.Join(argv, " "); got != tc.want {
				t.Errorf("argv = %q; want %q", got, tc.want)
			}
		})
	}
}

// Every row that olr will act on has to carry the words shown before it acts,
// and the id a client sends back. A row with a perform and no label would be a
// button with no text.
func TestEveryClearableBackendSaysWhatItWillDo(t *testing.T) {
	ids := map[string]bool{}
	for _, b := range DistroBackends {
		if !b.Clearable() {
			continue
		}
		if b.ActionID == "" || b.ActionLabel == "" || len(b.ActionRuns) == 0 {
			t.Errorf("%s can be cleared but does not say what that means: id=%q label=%q runs=%v",
				b.Unit, b.ActionID, b.ActionLabel, b.ActionRuns)
		}
		if ids[b.ActionID] {
			t.Errorf("%s reuses the action id %q, so one of the two is unreachable", b.Unit, b.ActionID)
		}
		ids[b.ActionID] = true

		// Every ActionRuns line must name the unit it belongs to. This is the
		// one drift that would be silent and serious: a row describing dnsmasq
		// and disabling something else.
		for _, line := range b.ActionRuns {
			if !strings.Contains(line, b.Unit) && !strings.Contains(line, resolvedDropIn) {
				t.Errorf("%s promises %q, which names neither the unit nor its drop-in", b.Unit, line)
			}
		}
	}
}

// systemd-resolved is the row that must never be stood down: the box resolves
// through it, including apt and olr's own upstream lookups, so stopping it takes
// name resolution away from the machine the operator is typing on.
func TestSystemdResolvedIsAskedToYieldAndNeverStopped(t *testing.T) {
	for _, b := range DistroBackends {
		if b.Unit != "systemd-resolved.service" {
			continue
		}
		if !b.Clearable() {
			t.Fatal("systemd-resolved has no action, so the one incumbent with a subtle answer gets none")
		}
		for _, line := range b.ActionRuns {
			if strings.Contains(line, "disable") || strings.Contains(line, "stop ") {
				t.Errorf("systemd-resolved's action promises %q, which stops the box's resolver", line)
			}
		}

		fake := withFakeUnits(t, map[string]UnitStatus{
			"systemd-resolved.service": {Installed: true, Active: true, Enabled: true},
		})
		dir := t.TempDir()
		prev := resolvedDropIn
		t.Cleanup(func() { resolvedDropIn = prev })
		resolvedDropIn = filepath.Join(dir, "10-olr-yield-53.conf")

		steps := b.action().do(context.Background())
		if StepsFailed(steps) {
			t.Fatalf("yielding failed: %+v", steps)
		}
		data, err := os.ReadFile(resolvedDropIn)
		if err != nil {
			t.Fatalf("no drop-in written: %v", err)
		}
		if !strings.Contains(string(data), "DNSStubListener=no") {
			t.Errorf("drop-in does not give up the socket: %q", data)
		}
		for _, act := range fake.acted {
			if strings.HasPrefix(act, "stop ") || strings.HasPrefix(act, "disable ") {
				t.Errorf("stopped the box's own resolver: %q", act)
			}
		}
		return
	}
	t.Fatal("systemd-resolved is not in the table at all")
}

// --- the blockers ----------------------------------------------------------

// A distribution olr does not recognise keeps exactly today's behaviour: text,
// no button. Guessing a package manager is worse than admitting we do not know,
// and guessing one and then *running* it is worse again.
func TestUnknownDistroGetsAdviceAndNoButton(t *testing.T) {
	withOSRelease(t, "ID=plan9\n")
	missing := Dependency{
		Tool:     "olr-no-such-backend",
		Why:      "olr does not implement it.",
		Packages: []Package{{Distro: "debian", Name: "the-package"}},
	}

	got := DependencyBlockers([]Dependency{missing})
	if len(got) != 1 {
		t.Fatalf("got %d blockers, want 1", len(got))
	}
	if got[0].Action != nil {
		t.Errorf("offered to run %+v on a distribution we cannot even advise", got[0].Action)
	}
	if got[0].Fix == "" {
		t.Error("dropped the advice as well as the button")
	}
}

func TestMissingDependencyOnDebianOffersToInstallIt(t *testing.T) {
	withOSRelease(t, debianOSRelease)
	withFakeUnits(t, nil)
	missing := Dependency{
		Tool:     "olr-no-such-backend",
		Why:      "olr does not implement it.",
		Packages: []Package{{Distro: "debian", Name: "the-package"}},
	}

	got := DependencyBlockers([]Dependency{missing})
	action := got[0].Action
	if action == nil {
		t.Fatal("no action on a distribution olr knows how to drive")
	}
	if action.ID != "install:olr-no-such-backend" {
		t.Errorf("id = %q", action.ID)
	}
	if len(action.Runs) != 1 || !strings.Contains(action.Runs[0], "the-package") {
		t.Errorf("runs = %q; want the install naming the package", action.Runs)
	}
}

// The whole point of the change, in one test. The two red panels an operator
// meets are one problem: Debian's unbound package starts unbound.service, so an
// install that stopped there would swap the first panel for the second.
func TestInstallPromisesAndPerformsTheStandDownItCauses(t *testing.T) {
	withOSRelease(t, debianOSRelease)
	fake := withFakeUnits(t, map[string]UnitStatus{})
	ran := withFakePackageManager(t, nil)

	dep := Dependency{
		Tool:     "olr-no-such-backend",
		Why:      "olr does not implement it.",
		Inert:    false,
		Shadows:  []string{"unbound.service"},
		Packages: []Package{{Distro: "debian", Name: "unbound"}},
	}

	action := installAction(dep, DetectDistro())
	if action == nil {
		t.Fatal("no action")
	}
	// Promised before it is pressed. A button that does two things and admits
	// to one is the version of this that loses the operator's trust.
	if len(action.Runs) != 2 || !strings.Contains(action.Runs[1], "disable --now unbound.service") {
		t.Fatalf("runs = %q; want the install and the stand-down", action.Runs)
	}
	if !strings.Contains(action.Label, ":53") {
		t.Errorf("label = %q; want it to say what olr gets out of it", action.Label)
	}

	// The package lands and, as on Debian, brings the unit up with it.
	fake.status["unbound.service"] = UnitStatus{Installed: true, Active: true, Enabled: true}

	steps := action.do(context.Background())
	if StepsFailed(steps) {
		t.Fatalf("steps failed: %+v", steps)
	}
	if len(*ran) != 1 || (*ran)[0][0] != "apt-get" {
		t.Fatalf("ran %v; want one apt-get", *ran)
	}
	if got := strings.Join(fake.acted, ", "); got != "stop unbound.service, disable unbound.service" {
		t.Errorf("did %q; want the unit stopped and disabled", got)
	}
}

// The other half of the same rule: whether the package starts anything is the
// distribution's decision, so the stand-down is conditional on what is actually
// live afterwards rather than on what Debian happens to do.
func TestInstallDoesNotStandDownAUnitThatNeverCameUp(t *testing.T) {
	withOSRelease(t, fedoraOSRelease)
	fake := withFakeUnits(t, map[string]UnitStatus{})
	withFakePackageManager(t, nil)

	dep := Dependency{
		Tool:     "olr-no-such-backend",
		Shadows:  []string{"unbound.service"},
		Packages: []Package{{Distro: "fedora", Name: "unbound"}},
	}

	steps := installAction(dep, DetectDistro()).do(context.Background())
	if StepsFailed(steps) {
		t.Fatalf("steps failed: %+v", steps)
	}
	if len(fake.acted) != 0 {
		t.Errorf("touched %q on a distribution that started nothing", fake.acted)
	}
	// Reported rather than skipped silently: the button promised the line, so
	// the answer to it belongs in the record even when the answer is "nothing".
	last := steps[len(steps)-1]
	if !strings.Contains(last.Description, "nothing to stand down") || !last.Done {
		t.Errorf("last step = %+v; want it to say there was nothing there", last)
	}
}

// A failed install must not be followed by a stand-down. The unit the
// stand-down is for is the one the install was meant to create, so acting after
// it failed is acting on a box we no longer have a description of.
func TestAFailedInstallStopsBeforeTheStandDown(t *testing.T) {
	withOSRelease(t, debianOSRelease)
	fake := withFakeUnits(t, map[string]UnitStatus{
		"unbound.service": {Installed: true, Active: true, Enabled: true},
	})
	withFakePackageManager(t, errors.New("exit status 100"))

	dep := Dependency{
		Tool:     "olr-no-such-backend",
		Shadows:  []string{"unbound.service"},
		Packages: []Package{{Distro: "debian", Name: "unbound"}},
	}

	steps := installAction(dep, DetectDistro()).do(context.Background())
	if !StepsFailed(steps) {
		t.Fatal("a failed install reported success")
	}
	if len(fake.acted) != 0 {
		t.Errorf("stood %q down after the install failed", fake.acted)
	}
	// The package manager's own words, which are the only useful thing about
	// any of the interesting failures here.
	if !strings.Contains(steps[0].Error, "fake package manager output") {
		t.Errorf("error = %q; want it to carry the output", steps[0].Error)
	}
}

// Not root is refused rather than escalated: shelling out to sudo has no tty
// here and would hang or fail obscurely.
func TestInstallRefusesWhenNotRoot(t *testing.T) {
	prev := geteuid
	t.Cleanup(func() { geteuid = prev })
	geteuid = func() int { return 1000 }

	steps := installPackage(context.Background(), []string{"apt-get", "install", "-y", "unbound"})
	if !StepsFailed(steps) || !strings.Contains(steps[0].Error, "root") {
		t.Errorf("steps = %+v; want a refusal naming root", steps)
	}
}

// --- selecting what to clear -----------------------------------------------

func TestFixBlockersRefusesAnIdNobodyOffered(t *testing.T) {
	blockers := []Blocker{{Kind: BlockerDistroBackend, Summary: "something", Action: &Action{
		ID: "standdown:dnsmasq.service", do: func(context.Context) []Step { return nil },
	}}}

	if _, err := FixBlockers(context.Background(), blockers, []string{"install:bind9"}); !errors.Is(err, ErrNoSuchFix) {
		t.Errorf("err = %v; want ErrNoSuchFix", err)
	}
}

// An empty list means everything, in the order the module reported it — which is
// the order it has to be worked through, since on Debian the missing package is
// what creates the port conflict.
func TestFixBlockersWithNoIdsDoesThemAllInOrder(t *testing.T) {
	var done []string
	step := func(name string) *Action {
		return &Action{ID: name, do: func(context.Context) []Step {
			done = append(done, name)
			return []Step{{Description: name, Done: true}}
		}}
	}
	blockers := []Blocker{
		{Summary: "first", Action: step("install:unbound")},
		{Summary: "second", Action: step("standdown:dnsmasq.service")},
		{Summary: "not ours"},
	}

	steps, err := FixBlockers(context.Background(), blockers, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(done, ","); got != "install:unbound,standdown:dnsmasq.service" {
		t.Errorf("ran %q, in that order", got)
	}
	if len(steps) != 2 {
		t.Errorf("got %d steps, want 2", len(steps))
	}
}

// A blocker olr cannot clear serialises and renders exactly as it did before
// any of this existed, which is the whole degradation story.
func TestActionableIgnoresBlockersWithNoAction(t *testing.T) {
	got := Actionable([]Blocker{{Summary: "a"}, {Summary: "b", Action: &Action{ID: "x"}}})
	if len(got) != 1 || got[0].Summary != "b" {
		t.Errorf("got %+v; want only the one with an action", got)
	}
}
