package devices

import (
	"github.com/open-linux-router/open-linux-router/internal/runtimedb"
	"path/filepath"
	"testing"
	"time"
)

func TestSeenSQLiteImportAndRetention(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "seen.json")
	now := time.Now()
	s := NewSeen(legacy, nil, nil)
	s.Record(now, []string{"aa:bb:cc:dd:ee:ff"})
	if err := s.Flush(now); err != nil {
		t.Fatal(err)
	}
	db, err := runtimedb.Open(filepath.Join(dir, "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s, err = OpenSeenDB(db, legacy, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if at, ok := s.LastSeen("aa:bb:cc:dd:ee:ff"); !ok || !at.Equal(now) {
		t.Fatal("import lost sighting")
	}
	if err := s.Flush(now.Add(Forget + time.Hour)); err != nil {
		t.Fatal(err)
	}
	s, err = OpenSeenDB(db, legacy, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.LastSeen("aa:bb:cc:dd:ee:ff"); ok {
		t.Fatal("expired record resurrected")
	}
}
