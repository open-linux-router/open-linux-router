package dhcp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeService records what was asked of the daemon without needing one.
type fakeService struct {
	active bool
	calls  []string
	fail   error
}

func (f *fakeService) Status(context.Context) (ServiceStatus, error) {
	return ServiceStatus{Unit: "olr-dhcp.service", Active: f.active, State: "active"}, nil
}

func (f *fakeService) do(verb string) error {
	f.calls = append(f.calls, verb)
	if f.fail != nil {
		return f.fail
	}
	switch verb {
	case "start", "restart":
		f.active = true
	case "stop":
		f.active = false
	}
	return nil
}

func (f *fakeService) Start(context.Context) error   { return f.do("start") }
func (f *fakeService) Stop(context.Context) error    { return f.do("stop") }
func (f *fakeService) Restart(context.Context) error { return f.do("restart") }
func (f *fakeService) Reload(context.Context) error  { return f.do("reload") }

// testApplier builds an Applier rooted entirely inside a temp directory, which
// is what makes the whole apply path testable without root, systemd or /etc.
func testApplier(t *testing.T) (Applier, *fakeService) {
	t.Helper()
	root := t.TempDir()
	paths := Paths{
		Conf:      filepath.Join(root, "rendered", "dnsmasq.conf"),
		HostsDir:  filepath.Join(root, "rendered", "hosts.d"),
		OptsDir:   filepath.Join(root, "rendered", "opts.d"),
		LeaseFile: filepath.Join(root, "state", "leases"),
		PIDFile:   filepath.Join(root, "run", "dhcp.pid"),
	}
	svc := &fakeService{}
	return Applier{
		Backend:    NewDnsmasq(paths),
		Links:      testLinks(),
		Service:    svc,
		Paths:      paths,
		ConfigPath: filepath.Join(root, "dhcp.json"),
		// Pinned rather than left to PortConflict, which would read the build
		// machine's /proc and make these tests depend on whether anything
		// happens to be serving DHCP there.
		PortCheck: func() (bool, error) { return false, nil },
	}, svc
}

func TestApplyOnAFreshSystem(t *testing.T) {
	a, svc := testApplier(t)
	c := validConfig(t)

	result, err := a.Apply(context.Background(), c)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if _, err := os.Stat(a.ConfigPath); err != nil {
		t.Errorf("intent was not stored: %v", err)
	}
	if _, err := os.Stat(a.Paths.Conf); err != nil {
		t.Errorf("dnsmasq.conf was not written: %v", err)
	}
	if want := []string{"start"}; !equalStrings(svc.calls, want) {
		t.Errorf("service calls = %v, want %v", svc.calls, want)
	}
	if result.Plan.Action != ActionStart {
		t.Errorf("Action = %q, want start", result.Plan.Action)
	}
	for _, step := range result.Steps {
		if !step.Done {
			t.Errorf("step %q did not complete: %s", step.Description, step.Error)
		}
	}
}

// Applying the same config twice must be a no-op. This is the property the
// whole drift story rests on: if a clean re-apply were not empty, `olr status`
// would report drift forever.
func TestApplyIsIdempotent(t *testing.T) {
	a, svc := testApplier(t)
	c := validConfig(t)
	ctx := context.Background()

	if _, err := a.Apply(ctx, c); err != nil {
		t.Fatalf("first Apply: %v", err)
	}
	before := len(svc.calls)

	result, err := a.Apply(ctx, c)
	if err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	if !result.Plan.Empty() {
		t.Errorf("re-applying an unchanged config planned work: %v", result.Plan.Changes)
	}
	if len(svc.calls) != before {
		t.Errorf("re-apply touched the service: %v", svc.calls[before:])
	}
}

func TestDriftIsDetectedWhenAFileIsEditedOutOfBand(t *testing.T) {
	a, _ := testApplier(t)
	ctx := context.Background()
	if _, err := a.Apply(ctx, validConfig(t)); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	clean, err := a.Drift(ctx)
	if err != nil {
		t.Fatalf("Drift: %v", err)
	}
	if !clean.Empty() {
		t.Fatalf("drift reported immediately after apply: %v", clean.Changes)
	}

	// Somebody edits the generated file the header told them not to edit.
	if err := os.WriteFile(a.Paths.Conf, []byte("port=53\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	drifted, err := a.Drift(ctx)
	if err != nil {
		t.Fatalf("Drift: %v", err)
	}
	if drifted.Empty() {
		t.Fatal("hand-edited config was not reported as drift")
	}
	if drifted.Action != ActionRestart {
		t.Errorf("Action = %q, want restart", drifted.Action)
	}
}

// A reservation left behind by an earlier config keeps being served, because
// dnsmasq reads the directory rather than our intent. Observe has to see it.
func TestObserveSeesFilesWeDidNotRender(t *testing.T) {
	a, _ := testApplier(t)
	ctx := context.Background()
	if _, err := a.Apply(ctx, validConfig(t)); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	stray := filepath.Join(a.Paths.HostsDir, "deadbeef0000.conf")
	if err := os.WriteFile(stray, []byte("de:ad:be:ef:00:00,192.168.1.77\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	obs, err := a.Observe(ctx)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if _, ok := obs.Files[stray]; !ok {
		t.Fatalf("Observe missed %s; saw %v", stray, keys(obs.Files))
	}

	// And applying must clean it up.
	result, err := a.Apply(ctx, validConfig(t))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if _, err := os.Stat(stray); !os.IsNotExist(err) {
		t.Errorf("stray reservation survived apply (err=%v), steps: %v", err, result.Steps)
	}
}

// design.md §5.3.2: no rollback. A failure leaves what landed in place and
// reports exactly which steps those were, so a re-run can finish the job.
func TestApplyReportsPartialProgressOnFailure(t *testing.T) {
	a, svc := testApplier(t)
	svc.fail = errors.New("unit olr-dhcp.service failed to start")

	result, err := a.Apply(context.Background(), validConfig(t))
	if err == nil {
		t.Fatal("Apply succeeded despite the service failing")
	}

	// The files that were written before the failure stay written.
	if _, statErr := os.Stat(a.Paths.Conf); statErr != nil {
		t.Errorf("config was rolled back rather than left in place: %v", statErr)
	}

	var failed, done int
	for _, s := range result.Steps {
		if s.Done {
			done++
		} else {
			failed++
			if !strings.Contains(s.Error, "failed to start") {
				t.Errorf("failed step does not carry the cause: %+v", s)
			}
		}
	}
	if done == 0 {
		t.Error("no completed steps reported; a re-run could not tell what landed")
	}
	if failed != 1 {
		t.Errorf("got %d failed steps, want 1", failed)
	}
}

// Intent is stored before the things it describes, so a re-run has something to
// finish the job from.
func TestApplyStoresIntentEvenWhenTheServiceFails(t *testing.T) {
	a, svc := testApplier(t)
	svc.fail = errors.New("boom")

	if _, err := a.Apply(context.Background(), validConfig(t)); err == nil {
		t.Fatal("expected an error")
	}

	stored, err := a.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(stored.Pools) != 1 || stored.Pools[0].Interface != "br-lan" {
		t.Errorf("intent was not stored before the failure: %+v", stored)
	}
}

func TestApplyRejectsAnInvalidConfigBeforeWritingAnything(t *testing.T) {
	a, svc := testApplier(t)
	c := validConfig(t)
	c.Pools[0].Start = addr(t, "10.0.0.5") // outside br-lan

	if _, err := a.Apply(context.Background(), c); err == nil {
		t.Fatal("Apply accepted an invalid config")
	}
	if _, err := os.Stat(a.ConfigPath); !os.IsNotExist(err) {
		t.Error("an invalid config was still stored")
	}
	if _, err := os.Stat(a.Paths.Conf); !os.IsNotExist(err) {
		t.Error("an invalid config was still rendered to disk")
	}
	if len(svc.calls) != 0 {
		t.Errorf("the service was touched for an invalid config: %v", svc.calls)
	}
}

func TestApplyReloadsForAReservation(t *testing.T) {
	a, svc := testApplier(t)
	ctx := context.Background()
	c := validConfig(t)
	if _, err := a.Apply(ctx, c); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	svc.calls = nil

	c.SetReservation(Reservation{MAC: "aa:bb:cc:dd:ee:ff", IP: addr(t, "192.168.1.50"), Hostname: "nas"})
	if _, err := a.Apply(ctx, c); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if want := []string{"reload"}; !equalStrings(svc.calls, want) {
		t.Errorf("service calls = %v, want %v — a reservation must not restart the daemon", svc.calls, want)
	}
}

// We run our own dnsmasq instance rather than taking over the distro's, so two
// DHCP servers can end up racing for UDP/67. olr refuses rather than stopping
// somebody else's daemon (design.md §3.4).
func TestApplyRefusesToStartWhenSomethingElseHoldsThePort(t *testing.T) {
	a, svc := testApplier(t)
	a.PortCheck = func() (bool, error) { return true, nil }

	result, err := a.Apply(context.Background(), validConfig(t))
	if err == nil {
		t.Fatal("Apply started a second DHCP server on an occupied port")
	}
	if !strings.Contains(err.Error(), "UDP/67") {
		t.Errorf("error does not name the conflict: %v", err)
	}
	if !strings.Contains(err.Error(), "ss -lunp") {
		t.Errorf("error does not tell the operator how to find the holder: %v", err)
	}
	if len(svc.calls) != 0 {
		t.Errorf("the service was started anyway: %v", svc.calls)
	}
	// The check must come after the files are written, so that fixing the
	// conflict and re-running is a pure start with nothing left to render.
	if _, statErr := os.Stat(a.Paths.Conf); statErr != nil {
		t.Errorf("config was not written before the port check: %v", statErr)
	}
	if len(result.Steps) == 0 {
		t.Error("no steps recorded")
	}
}

// A reload must not be gated on the port being free — the daemon we are about
// to signal is the very thing holding it.
func TestPortCheckDoesNotBlockAReload(t *testing.T) {
	a, svc := testApplier(t)
	ctx := context.Background()
	c := validConfig(t)
	if _, err := a.Apply(ctx, c); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	a.PortCheck = func() (bool, error) { return true, nil } // our own daemon
	svc.calls = nil

	c.SetReservation(Reservation{MAC: "aa:bb:cc:dd:ee:ff", IP: addr(t, "192.168.1.50")})
	if _, err := a.Apply(ctx, c); err != nil {
		t.Fatalf("reload was blocked by our own daemon holding the port: %v", err)
	}
	if want := []string{"reload"}; !equalStrings(svc.calls, want) {
		t.Errorf("service calls = %v, want %v", svc.calls, want)
	}
}

func TestWriteFileAtomicReplacesAndLeavesNoTemporaries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "file.conf")

	if err := writeFileAtomic(path, []byte("first\n"), 0o644); err != nil {
		t.Fatalf("writeFileAtomic: %v", err)
	}
	if err := writeFileAtomic(path, []byte("second\n"), 0o600); err != nil {
		t.Fatalf("writeFileAtomic: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "second\n" {
		t.Errorf("content = %q, want %q", data, "second\n")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", info.Mode().Perm())
	}

	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("temporary files left behind: %v", names)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
