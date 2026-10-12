package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// The watch owns its ticker, so the tests drive it by running it fast and
// synchronising on what it does rather than on how long it took.
const testTick = time.Millisecond

// fakeBackend is a backend whose answers a test can change while the watch is
// running, with every field guarded because the watch reads them from its own
// goroutine.
type fakeBackend struct {
	mu       sync.Mutex
	pending  bool
	readErr  error
	startErr error
	starts   int
	// healed makes a successful start report the backend as running again,
	// which is what a real one does.
	healed bool

	attempted chan struct{}
}

func newFakeBackend(pending bool) *fakeBackend {
	return &fakeBackend{pending: pending, healed: true, attempted: make(chan struct{}, 64)}
}

func (f *fakeBackend) backend() backend {
	return backend{
		name: "fake",
		pending: func(context.Context) (bool, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			return f.pending, f.readErr
		},
		start: func(context.Context) error {
			f.mu.Lock()
			f.starts++
			err := f.startErr
			if err == nil && f.healed {
				f.pending = false
			}
			f.mu.Unlock()

			select {
			case f.attempted <- struct{}{}:
			default:
			}
			return err
		},
	}
}

func (f *fakeBackend) set(fn func(*fakeBackend)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

func (f *fakeBackend) startCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.starts
}

// waitForAttempts blocks until the watch has tried to start something n times.
func waitForAttempts(t *testing.T, f *fakeBackend, n int) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for i := 0; i < n; i++ {
		select {
		case <-f.attempted:
		case <-deadline:
			t.Fatalf("timed out after %d of %d start attempts", i, n)
		}
	}
}

// runWatch starts the watch and stops it when the test ends.
func runWatch(t *testing.T, b backend) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go superviseBackends(ctx, []backend{b}, core.NewLock(), testTick, quietLogger())
}

// The whole point: intent says it should be running, it is not, and nobody had
// to notice.
func TestWatchStartsABackendThatIsNotRunning(t *testing.T) {
	f := newFakeBackend(true)
	runWatch(t, f.backend())

	waitForAttempts(t, f, 1)

	// It came up, so the watch has nothing left to do. Give it room to prove
	// it is not starting a running daemon over and over.
	time.Sleep(50 * testTick)
	if got := f.startCount(); got != 1 {
		t.Errorf("started %d times, want 1 — the watch is acting on a healthy backend", got)
	}
}

func TestWatchLeavesAHealthyBackendAlone(t *testing.T) {
	f := newFakeBackend(false)
	runWatch(t, f.backend())

	time.Sleep(50 * testTick)
	if got := f.startCount(); got != 0 {
		t.Errorf("started a backend that was already running %d times", got)
	}
}

// A backend that cannot be started is a fault olr cannot fix — a config it will
// not parse, a port somebody else holds. Retrying forever would bury the reason
// in a log nobody can read and fight systemd's own restart counter for the same
// unit.
func TestWatchGivesUpAfterRepeatedFailures(t *testing.T) {
	f := newFakeBackend(true)
	f.set(func(f *fakeBackend) { f.startErr = errors.New("unbound will not parse its config") })
	runWatch(t, f.backend())

	waitForAttempts(t, f, giveUpAfter)

	time.Sleep(50 * testTick)
	if got := f.startCount(); got != giveUpAfter {
		t.Errorf("tried %d times, want %d — the watch did not give up", got, giveUpAfter)
	}
}

// Giving up is not permanent, and it must not need an olrd restart to undo.
// Somebody fixes the cause and starts the unit; the watch sees it healthy and
// puts it back under supervision.
func TestWatchResumesOnceTheBackendIsSeenRunningAgain(t *testing.T) {
	f := newFakeBackend(true)
	f.set(func(f *fakeBackend) { f.startErr = errors.New("nope") })
	runWatch(t, f.backend())

	waitForAttempts(t, f, giveUpAfter)

	// Fixed by hand, and running.
	f.set(func(f *fakeBackend) { f.pending, f.startErr = false, nil })
	time.Sleep(20 * testTick)

	// And down again later, for a reason the watch can do something about.
	f.set(func(f *fakeBackend) { f.pending = true })

	waitForAttempts(t, f, 1)
	if got := f.startCount(); got <= giveUpAfter {
		t.Errorf("started %d times, want more than %d — the watch never resumed", got, giveUpAfter)
	}
}

// A system bus that was briefly unavailable is not the backend's fault and must
// not use up one of its attempts.
func TestWatchDoesNotCountAFailedReadAgainstTheBackend(t *testing.T) {
	f := newFakeBackend(true)
	f.set(func(f *fakeBackend) { f.readErr = errors.New("no system bus") })
	runWatch(t, f.backend())

	time.Sleep(50 * testTick)
	if got := f.startCount(); got != 0 {
		t.Fatalf("acted on a box it could not read, %d times", got)
	}

	// The bus comes back. The backend still has all of its attempts.
	f.set(func(f *fakeBackend) { f.readErr = nil })
	waitForAttempts(t, f, 1)
}

func TestRecoveryCancellationWhileWaitingDoesNotReadOrStart(t *testing.T) {
	lock := core.NewLock()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := backend{
		pending: func(context.Context) (bool, error) {
			t.Error("read eligibility before acquiring the lock")
			return true, nil
		},
		start: func(context.Context) error {
			t.Error("started recovery without the lock")
			return nil
		},
	}
	if err := lock.Do(context.Background(), func() error {
		cancel()
		attempted, err := recoverBackend(ctx, b, lock)
		if attempted || !errors.Is(err, context.Canceled) {
			t.Errorf("attempted=%v error=%v, want no attempt and cancellation", attempted, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRecoveryFailedRecheckDoesNotConsumeAttempt(t *testing.T) {
	f := newFakeBackend(true)
	f.readErr = errors.New("no system bus")
	attempted, err := recoverBackend(context.Background(), f.backend(), core.NewLock())
	if attempted || !errors.Is(err, f.readErr) || f.startCount() != 0 {
		t.Fatalf("attempted=%v starts=%d error=%v, want only the read error", attempted, f.startCount(), err)
	}
}

func TestRecoveryUsesCurrentIntentAndSerializesTheWholeApply(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("enabled=%v", enabled), func(t *testing.T) {
			lock := core.NewLock()
			store := core.NewStore(filepath.Join(t.TempDir(), "olr.json"), "backend", "devices")
			doc, err := store.Load()
			if err != nil {
				t.Fatal(err)
			}
			doc.Set("backend", []byte(`{"enabled":true,"label":"old"}`))
			if err := store.Save(doc); err != nil {
				t.Fatal(err)
			}
			load := func() (bool, string, error) {
				doc, err := store.Load()
				if err != nil {
					return false, "", err
				}
				raw, _ := doc.Raw("backend")
				var cfg struct {
					Enabled bool
					Label   string
				}
				err = json.Unmarshal(raw, &cfg)
				return cfg.Enabled, cfg.Label, err
			}
			started, release := make(chan struct{}), make(chan struct{})
			defer close(release)
			b := backend{
				pending: func(context.Context) (bool, error) {
					if acquired, _ := lock.TryDo(func() error { return nil }); acquired {
						return false, errors.New("eligibility check did not hold the apply lock")
					}
					on, _, err := load()
					return on, err
				},
				start: func(context.Context) error {
					_, label, err := load()
					if err != nil {
						return err
					}
					if label != "edited" {
						return fmt.Errorf("recovery read stale intent: %q", label)
					}
					doc, err := store.Load()
					if err != nil {
						return err
					}
					close(started)
					<-release
					doc.Set("backend", []byte(`{"enabled":true,"label":"recovered edited"}`))
					return store.Save(doc)
				},
			}
			// A poll saw the old enabled intent. While it waits for a writer,
			// that writer changes this module and an unrelated module together.
			if pending, _, err := load(); err != nil || !pending {
				t.Fatal("initial recovery hint", pending, err)
			}
			type result struct {
				attempted bool
				err       error
			}
			finished := make(chan result, 1)
			if err := lock.Do(context.Background(), func() error {
				doc.Set("backend", []byte(fmt.Sprintf(`{"enabled":%v,"label":"edited"}`, enabled)))
				doc.Set("devices", []byte(`{"name":"concurrent edit"}`))
				if err := store.Save(doc); err != nil {
					return err
				}
				go func() {
					attempted, err := recoverBackend(context.Background(), b, lock)
					finished <- result{attempted, err}
				}()
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if enabled {
				select {
				case <-started:
				case got := <-finished:
					t.Fatalf("recovery did not reach apply: %+v", got)
				case <-time.After(5 * time.Second):
					t.Fatal("recovery did not start")
				}
				if acquired, _ := lock.TryDo(func() error { return nil }); acquired {
					t.Error("another writer could interleave with recovery apply")
				}
				release <- struct{}{}
			}
			select {
			case got := <-finished:
				if got.err != nil || got.attempted != enabled {
					t.Fatalf("attempted=%v error=%v, want attempted=%v", got.attempted, got.err, enabled)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("recovery did not finish")
			}
			doc, err = store.Load()
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := doc.Raw("devices")
			var device struct{ Name string }
			if err := json.Unmarshal(raw, &device); err != nil || device.Name != "concurrent edit" {
				t.Fatalf("unrelated config edit lost: %s, error=%v", raw, err)
			}
			_, label, err := load()
			want := "edited"
			if enabled {
				want = "recovered edited"
			}
			if err != nil || label != want {
				t.Fatalf("label=%q error=%v, want %q", label, err, want)
			}
		})
	}
}

// The policy of the whole file, stated as a table because each line is a
// separate promise about what olrd will do without being asked.
func TestStartIsTheWholeJob(t *testing.T) {
	for _, tc := range []struct {
		name                              string
		starts, rewritesFiles, disruptive bool
		want                              bool
	}{
		{"nothing is down", false, false, false, false},
		{"down, and starting it is all it needs", true, false, false, true},
		{"down, but a rendered file was also changed", true, true, false, false},
		{"down, and putting it back would cut somebody off", true, false, true, false},
		{"a pending edit on a healthy backend", false, true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := startIsTheWholeJob(tc.starts, tc.rewritesFiles, tc.disruptive); got != tc.want {
				t.Errorf("startIsTheWholeJob(%v, %v, %v) = %v, want %v",
					tc.starts, tc.rewritesFiles, tc.disruptive, got, tc.want)
			}
		})
	}
}
