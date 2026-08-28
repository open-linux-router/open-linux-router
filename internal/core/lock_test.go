package core

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestLockSerialisesWriters(t *testing.T) {
	l := NewLock()

	var mu sync.Mutex
	concurrent, peak := 0, 0

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = l.Do(context.Background(), func() error {
				mu.Lock()
				concurrent++
				if concurrent > peak {
					peak = concurrent
				}
				mu.Unlock()

				time.Sleep(time.Millisecond)

				mu.Lock()
				concurrent--
				mu.Unlock()
				return nil
			})
		}()
	}
	wg.Wait()

	if peak != 1 {
		t.Errorf("peak concurrency = %d, want 1", peak)
	}
}

func TestLockIsReleasedAfterAFailingFunction(t *testing.T) {
	l := NewLock()
	want := errors.New("boom")

	if got := l.Do(context.Background(), func() error { return want }); !errors.Is(got, want) {
		t.Fatalf("Do returned %v, want %v", got, want)
	}
	if l.held() {
		t.Error("lock still held after the function returned an error")
	}
}

// An apply that panics must not wedge every future write. olrd is resident and
// holds the admin API; a leaked lock would be indistinguishable from a hang.
func TestLockIsReleasedAfterAPanic(t *testing.T) {
	l := NewLock()

	func() {
		defer func() { _ = recover() }()
		_ = l.Do(context.Background(), func() error { panic("boom") })
	}()

	if l.held() {
		t.Fatal("lock still held after a panic")
	}
	if err := l.Do(context.Background(), func() error { return nil }); err != nil {
		t.Errorf("lock unusable after a panic: %v", err)
	}
}

// A client that gave up waiting should stop waiting. sync.Mutex cannot be
// cancelled, which is why Lock is a semaphore.
func TestLockRespectsContextCancellation(t *testing.T) {
	l := NewLock()

	release := make(chan struct{})
	holding := make(chan struct{})
	go func() {
		_ = l.Do(context.Background(), func() error {
			close(holding)
			<-release
			return nil
		})
	}()
	<-holding
	defer close(release)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	ran := false
	err := l.Do(ctx, func() error { ran = true; return nil })

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want DeadlineExceeded", err)
	}
	if ran {
		t.Error("the function ran despite the context expiring")
	}
}

func TestTryDoReportsWhenTheLockIsBusy(t *testing.T) {
	l := NewLock()

	release := make(chan struct{})
	holding := make(chan struct{})
	go func() {
		_ = l.Do(context.Background(), func() error {
			close(holding)
			<-release
			return nil
		})
	}()
	<-holding

	if ran, _ := l.TryDo(func() error { return nil }); ran {
		t.Error("TryDo ran while the lock was held")
	}

	close(release)
	// Wait for the holder to actually let go before asserting the free case.
	for l.held() {
		time.Sleep(time.Millisecond)
	}
	if ran, _ := l.TryDo(func() error { return nil }); !ran {
		t.Error("TryDo did not run with the lock free")
	}
}
