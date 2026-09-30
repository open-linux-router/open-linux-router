package devices

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

type heardFunc func() ([]string, error)

func (f heardFunc) Heard(context.Context) ([]string, error) { return f() }

func TestSeenRecordsAndSurvivesARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices", "seen.json")
	at := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

	s := NewSeen(path, heardFunc(func() ([]string, error) { return []string{"AA-BB-CC-DD-EE-01"}, nil }), nil)
	s.Sample(context.Background(), at)
	if err := s.Flush(at); err != nil {
		t.Fatal(err)
	}

	again := NewSeen(path, nil, nil)
	got, ok := again.LastSeen("aa:bb:cc:dd:ee:01")
	if !ok || !got.Equal(at) {
		t.Fatalf("after a restart got %v %v, want %v", got, ok, at)
	}
}

func TestSeenNeverMovesBackwards(t *testing.T) {
	s := NewSeen("", nil, nil)
	later := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	s.Record(later, []string{"aa:bb:cc:dd:ee:01"})
	s.Record(later.Add(-time.Hour), []string{"aa:bb:cc:dd:ee:01"})
	if got, _ := s.LastSeen("aa:bb:cc:dd:ee:01"); !got.Equal(later) {
		t.Fatalf("got %v, want %v", got, later)
	}
}

func TestSeenForgetsWhatIsLongGone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "seen.json")
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	s := NewSeen(path, nil, nil)
	s.Record(now.Add(-Forget-time.Hour), []string{"aa:bb:cc:dd:ee:01"})
	s.Record(now, []string{"aa:bb:cc:dd:ee:02"})
	if err := s.Flush(now); err != nil {
		t.Fatal(err)
	}
	again := NewSeen(path, nil, nil)
	if _, ok := again.LastSeen("aa:bb:cc:dd:ee:01"); ok {
		t.Error("a device gone past Forget is still on the record")
	}
	if _, ok := again.LastSeen("aa:bb:cc:dd:ee:02"); !ok {
		t.Error("a device heard now was dropped")
	}
}

func TestSeenStartsOverOnAnUnreadableFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "seen.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewSeen(path, nil, nil)
	s.Record(time.Now(), []string{"aa:bb:cc:dd:ee:01"})
	if err := s.Flush(time.Now()); err != nil {
		t.Fatalf("could not write over the unreadable file: %v", err)
	}
}

func TestSeenAFailingSourceMovesNothing(t *testing.T) {
	s := NewSeen("", heardFunc(func() ([]string, error) { return nil, errors.New("no netlink") }), nil)
	s.Sample(context.Background(), time.Now())
	s.Sample(context.Background(), time.Now())
	if _, ok := s.LastSeen("aa:bb:cc:dd:ee:01"); ok {
		t.Fatal("a failed read recorded something")
	}
}

func TestListCarriesLastSeen(t *testing.T) {
	store := core.NewStore(filepath.Join(t.TempDir(), "olr.json"), ModuleName)
	at := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	seen := NewSeen("", nil, nil)
	seen.Record(at, []string{"aa:bb:cc:dd:ee:01"})
	a := Applier{
		Store:    store,
		Presence: []PresenceSource{fixedSource{arp("aa:bb:cc:dd:ee:01", "192.168.1.5", true), arp("aa:bb:cc:dd:ee:02", "192.168.1.6", true)}},
		Seen:     seen,
	}
	list, _, err := a.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	byMAC := map[string]Resolved{}
	for _, r := range list {
		byMAC[r.MAC] = r
	}
	if got := byMAC["aa:bb:cc:dd:ee:01"].LastSeen; got == nil || !got.Equal(at) {
		t.Errorf("heard device: last seen %v, want %v", got, at)
	}
	if got := byMAC["aa:bb:cc:dd:ee:02"].LastSeen; got != nil {
		t.Errorf("unheard device: last seen %v, want none", got)
	}
}

type fixedSource []Sighting

func (fixedSource) Name() Source { return SourceARP }

func (f fixedSource) Presence(context.Context) ([]Sighting, []Problem, error) { return f, nil, nil }
