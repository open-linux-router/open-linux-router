package daemon

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
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
	go superviseBackends(ctx, []backend{b}, testTick, quietLogger())
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
