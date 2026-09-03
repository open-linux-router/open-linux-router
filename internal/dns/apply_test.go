package dns

import (
	"context"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// fakeService records what was asked of a daemon without needing one.
type fakeService struct {
	unit   string
	active bool
	calls  []string
	fail   error

	// enabled is the boot-time state, tracked separately from active because
	// that is exactly the distinction under test: a unit can be running now and
	// still not come back after a reboot.
	enabled bool

	// notInstalled models a box where the unit file was never laid down, which
	// is what an incomplete install looks like.
	notInstalled bool

	// statusErr is returned by Status instead of a state.
	statusErr error

	// diesAfter makes the unit go inactive after this many Status calls,
	// modelling a backend that accepts the start job and then exits — what
	// unbound does with a config it will not parse, by which time systemd has
	// already reported the job done.
	diesAfter int
	statusN   int
}

func (f *fakeService) Status(context.Context) (ServiceStatus, error) {
	if f.statusErr != nil {
		return ServiceStatus{Unit: f.unit}, f.statusErr
	}
	f.statusN++
	if f.diesAfter > 0 && f.statusN > f.diesAfter {
		f.active = false
		return ServiceStatus{
			Unit: f.unit, Active: false, State: "failed", SubState: "failed",
			Enabled: f.enabled, Installed: !f.notInstalled,
		}, nil
	}
	return ServiceStatus{
		Unit: f.unit, Active: f.active, State: "active",
		Enabled: f.enabled, Installed: !f.notInstalled,
	}, nil
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
	case "enable":
		f.enabled = true
	case "disable":
		f.enabled = false
	}
	return nil
}

func (f *fakeService) Start(context.Context) error   { return f.do("start") }
func (f *fakeService) Stop(context.Context) error    { return f.do("stop") }
func (f *fakeService) Restart(context.Context) error { return f.do("restart") }
func (f *fakeService) Reload(context.Context) error  { return f.do("reload") }
func (f *fakeService) Enable(context.Context) error  { return f.do("enable") }
func (f *fakeService) Disable(context.Context) error { return f.do("disable") }

// fakeObserver stands in for a running relay.
type fakeObserver struct {
	clients []Client
	err     error
}

func (f fakeObserver) Queries(context.Context) ([]Query, Stats, error) {
	return nil, Stats{}, f.err
}
func (f fakeObserver) Names(context.Context) ([]Name, Stats, error) { return nil, Stats{}, f.err }
func (f fakeObserver) Clients(context.Context) ([]Client, error)    { return f.clients, f.err }

// testApplier builds an Applier rooted entirely inside a temp directory, which
// is what makes the whole apply path testable without root, systemd or /etc.
func testApplier(t *testing.T) (Applier, *fakeService, *fakeService) {
	t.Helper()
	root := t.TempDir()

	paths := RootedPaths(root)
	store := core.NewStore(core.RootedConfigPath(root), ModuleName)
	if err := os.MkdirAll(filepath.Dir(store.Path()), 0o755); err != nil {
		t.Fatal(err)
	}

	backend := NewBackend(paths).WithSource(store.Path())
	resolver := &fakeService{unit: backend.ResolverUnit()}
	relay := &fakeService{unit: backend.RelayUnit()}

	return Applier{
		Backend:  backend,
		Links:    testLinks(),
		Resolver: resolver,
		Relay:    relay,
		Paths:    paths,
		Store:    store,
		// Negative means check once and return, so the settle window does not
		// cost the suite a second and a half per apply.
		Settle:    -1,
		PortCheck: func() (bool, error) { return false, nil },
	}, resolver, relay
}

func TestApplyWritesEverythingAndStartsBothBackends(t *testing.T) {
	a, resolver, relay := testApplier(t)
	cfg := validConfig()
	cfg.Policies = []Policy{{Name: "kids", Block: []string{"example.com"}}}
	cfg.Normalize()

	result, err := a.Apply(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Apply: %v\nsteps: %+v", err, result.Steps)
	}

	for _, path := range []string{
		a.Paths.UnboundConf,
		a.Paths.RelayConf,
		filepath.Join(a.Paths.PolicyDir, "kids.json"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s was not written: %v", path, err)
		}
	}
	if !contains(resolver.calls, "start") {
		t.Errorf("the resolver was not started: %v", resolver.calls)
	}
	if !contains(relay.calls, "start") {
		t.Errorf("the relay was not started: %v", relay.calls)
	}

	// Intent is stored first, so a failure later leaves a re-run something to
	// finish from (design.md §5.3.2).
	stored, err := a.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !stored.Enabled || len(stored.Policies) != 1 {
		t.Errorf("intent was not stored: %+v", stored)
	}
}

// Coming up, the resolver goes first: a relay whose upstream is not answering
// serves SERVFAIL to the whole network, and clients cache that.
func TestApplyStartsTheResolverBeforeTheRelay(t *testing.T) {
	a, _, _ := testApplier(t)

	result, err := a.Apply(context.Background(), validConfig())
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	resolverAt, relayAt := -1, -1
	for i, s := range result.Steps {
		switch {
		case strings.HasPrefix(s.Description, "start "+a.Backend.ResolverUnit()):
			resolverAt = i
		case strings.HasPrefix(s.Description, "start "+a.Backend.RelayUnit()):
			relayAt = i
		}
	}
	if resolverAt < 0 || relayAt < 0 {
		t.Fatalf("both units should have been started: %+v", result.Steps)
	}
	if resolverAt > relayAt {
		t.Errorf("the relay was started before its upstream:\n%+v", result.Steps)
	}
}

// Going down the order reverses, so nobody is handed a failure that looks like
// a broken name rather than a stopped service.
func TestApplyStopsTheRelayBeforeTheResolver(t *testing.T) {
	a, resolver, relay := testApplier(t)
	if _, err := a.Apply(context.Background(), validConfig()); err != nil {
		t.Fatal(err)
	}
	resolver.calls, relay.calls = nil, nil

	off := validConfig()
	off.Enabled = false
	result, err := a.Apply(context.Background(), off)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	resolverAt, relayAt := -1, -1
	for i, s := range result.Steps {
		switch {
		case strings.HasPrefix(s.Description, "stop "+a.Backend.ResolverUnit()):
			resolverAt = i
		case strings.HasPrefix(s.Description, "stop "+a.Backend.RelayUnit()):
			relayAt = i
		}
	}
	if resolverAt < 0 || relayAt < 0 {
		t.Fatalf("both units should have been stopped: %+v", result.Steps)
	}
	if relayAt > resolverAt {
		t.Errorf("the resolver was stopped before the relay in front of it:\n%+v", result.Steps)
	}
}

func TestApplyIsIdempotent(t *testing.T) {
	a, resolver, relay := testApplier(t)
	cfg := validConfig()

	if _, err := a.Apply(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	resolver.calls, relay.calls = nil, nil

	result, err := a.Apply(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolver.calls) != 0 || len(relay.calls) != 0 {
		t.Errorf("a second identical apply signalled the daemons: %v %v", resolver.calls, relay.calls)
	}
	// Storing intent is always attempted, so there is exactly one step.
	if len(result.Steps) != 1 {
		t.Errorf("a no-op apply did %d steps: %+v", len(result.Steps), result.Steps)
	}
}

// A blocklist edit must be a SIGHUP to the relay and nothing at all to unbound.
func TestApplyReloadsForAPolicyChange(t *testing.T) {
	a, resolver, relay := testApplier(t)
	if _, err := a.Apply(context.Background(), validConfig()); err != nil {
		t.Fatal(err)
	}
	resolver.calls, relay.calls = nil, nil

	cfg := validConfig()
	cfg.Policies = []Policy{{Name: "kids", Block: []string{"example.com"}}}
	cfg.Normalize()
	if _, err := a.Apply(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}

	if !contains(relay.calls, "reload") {
		t.Errorf("the relay was not reloaded: %v", relay.calls)
	}
	if len(resolver.calls) != 0 {
		t.Errorf("a blocklist edit touched the resolver: %v", resolver.calls)
	}
}

// systemd-resolved holds :53 on a great many Debian boxes without anybody
// having chosen it, so this is the difference between a clear message and a
// relay that flaps.
func TestApplyRefusesToStartWhenSomethingElseHoldsThePort(t *testing.T) {
	a, _, relay := testApplier(t)
	a.PortCheck = func() (bool, error) { return true, nil }

	_, err := a.Apply(context.Background(), validConfig())
	if err == nil {
		t.Fatal("apply succeeded with the port already taken")
	}
	if !strings.Contains(err.Error(), "53") {
		t.Errorf("the refusal does not name the port: %v", err)
	}
	if contains(relay.calls, "start") {
		t.Errorf("the relay was started anyway: %v", relay.calls)
	}
}

// The check speaks to port 53 and nothing else. An operator who moved the relay
// has taken responsibility for whatever port they chose.
func TestApplySkipsThePortCheckOnANonDefaultPort(t *testing.T) {
	a, _, relay := testApplier(t)
	a.PortCheck = func() (bool, error) {
		t.Error("the port check ran for a listener that is not on 53")
		return true, nil
	}

	cfg := validConfig()
	cfg.Listen = []netip.AddrPort{netip.MustParseAddrPort("192.168.1.1:5300")}
	cfg.Normalize()

	if _, err := a.Apply(context.Background(), cfg); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !contains(relay.calls, "start") {
		t.Errorf("the relay was not started: %v", relay.calls)
	}
}

// A missing unit file is a packaging problem, and D-Bus's bare "unit not found"
// names neither the cause nor the fix.
func TestApplyRefusesToDriveAnUninstalledUnit(t *testing.T) {
	a, _, relay := testApplier(t)
	relay.notInstalled = true

	_, err := a.Apply(context.Background(), validConfig())
	if err == nil {
		t.Fatal("apply succeeded against a unit that is not installed")
	}
	if !strings.Contains(err.Error(), "Reinstall the package") {
		t.Errorf("the error does not say how to fix it: %v", err)
	}
}

// A backend that accepts the start job and then exits looks identical to a
// healthy start for the first fraction of a second — and here that is the whole
// network's name resolution.
func TestApplyFailsWhenABackendDoesNotStayUp(t *testing.T) {
	a, resolver, _ := testApplier(t)
	resolver.diesAfter = 1

	result, err := a.Apply(context.Background(), validConfig())
	if err == nil {
		t.Fatal("apply succeeded against a backend that exited")
	}
	if !strings.Contains(err.Error(), "did not stay up") {
		t.Errorf("unexpected error: %v", err)
	}
	// There is no rollback, so which steps landed is the operator's starting
	// point (design.md §5.3.2).
	if len(result.Steps) == 0 {
		t.Error("no steps were reported after a failure")
	}
}

// "We cannot tell" must not be reported as "it broke". Failing an apply on a
// developer box with no D-Bus would make the whole path untestable.
func TestApplyToleratesAMissingServiceManager(t *testing.T) {
	a, resolver, relay := testApplier(t)
	resolver.statusErr = ErrNoServiceManager
	relay.statusErr = ErrNoServiceManager

	if _, err := a.Apply(context.Background(), validConfig()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
}

// Failures degrade rather than propagate: a stopped relay has nothing to say,
// and a plan that refused to be built because of it would be useless at exactly
// the moment it is needed — when the operator is trying to fix DNS.
func TestObserveToleratesAnUnreachableRelay(t *testing.T) {
	a, _, _ := testApplier(t)
	a.Observer = fakeObserver{err: errors.New("connection refused")}

	obs, err := a.Observe(context.Background())
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if len(obs.Clients) != 0 {
		t.Errorf("clients were invented from a failed read: %v", obs.Clients)
	}
}

// A file left behind anywhere in the rendered tree — by an older olr, or by a
// hand-edit — is exactly the drift §5.4 exists to surface.
func TestObserveSeesFilesWeDidNotRender(t *testing.T) {
	a, _, _ := testApplier(t)
	if _, err := a.Apply(context.Background(), validConfig()); err != nil {
		t.Fatal(err)
	}

	stray := filepath.Join(a.Paths.PolicyDir, "left-behind.json")
	if err := os.WriteFile(stray, []byte(`{"name":"left-behind"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	obs, err := a.Observe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := obs.Files[stray]; !ok {
		t.Errorf("a stray policy file was not observed; saw %v", keys(obs.Files))
	}

	drift, err := a.Drift(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if drift.Empty() {
		t.Error("a stray policy file did not read as drift, so it would go on being enforced")
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
