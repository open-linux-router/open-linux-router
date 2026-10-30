package runtimedb

import (
	"database/sql"
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRestartTransactionsAndUnsignedCounters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := s.SaveActivity([]Activity{{Bucket: now.Unix(), Address: "192.0.2.1", Up: math.MaxUint64, Down: 42}}, 0); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSeen([]Seen{{MAC: "aa:bb:cc:dd:ee:ff", At: now, Hostname: "printer", HostnameAt: now}}); err != nil {
		t.Fatal(err)
	}
	key := [32]byte{1, 2, 3}
	if err := s.SaveSessions([]Session{{Key: key, Credential: "hash", Expires: now.Add(time.Hour)}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveQoS("boot-a", []byte(`{"queues":[]}`)); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		info, err := os.Stat(path + suffix)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("sidecar permissions %s: %v %v", suffix, info, err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rows, err := s.Activity(0, now.Unix())
	if err != nil || len(rows) != 1 || rows[0].Up != math.MaxUint64 {
		t.Fatalf("activity: %+v %v", rows, err)
	}
	seen, err := s.Seen()
	if err != nil || len(seen) != 1 || !seen[0].At.Equal(now) {
		t.Fatalf("seen: %+v %v", seen, err)
	}
	if seen[0].Hostname != "printer" || !seen[0].HostnameAt.Equal(now) {
		t.Fatalf("remembered name: %+v", seen[0])
	}
	sessions, err := s.Sessions()
	if err != nil || len(sessions) != 1 || sessions[0].Key != key {
		t.Fatalf("sessions: %+v %v", sessions, err)
	}
	if data, err := s.QoS("boot-b"); err != nil || data != nil {
		t.Fatal("restored another boot's ownership")
	}
	if err := s.SaveSessions(nil); err != nil {
		t.Fatal(err)
	}
	if rows, err := s.Sessions(); err != nil || len(rows) != 0 {
		t.Fatal("revocation not committed")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("database permissions", err)
	}
}

func TestImportRollbackAndNoResurrection(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	legacy := filepath.Join(t.TempDir(), "seen.json")
	if err := os.WriteFile(legacy, []byte("legacy"), 0600); err != nil {
		t.Fatal(err)
	}
	decode := func(_ []byte, tx *sql.Tx) error {
		if err := PutSeen(tx, []Seen{{MAC: "a", At: time.Now()}}); err != nil {
			return err
		}
		return errors.New("invalid source")
	}
	if err := s.Import("seen", legacy, decode); err == nil {
		t.Fatal("failed import succeeded")
	}
	if rows, err := s.Seen(); err != nil || len(rows) != 0 {
		t.Fatal("partial import committed")
	}
	if err := s.Import("seen", legacy, func(_ []byte, tx *sql.Tx) error { return PutSeen(tx, []Seen{{MAC: "a", At: time.Now()}}) }); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSeen(nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Import("seen", legacy, decode); err != nil {
		t.Fatal("legacy reread", err)
	}
	if rows, err := s.Seen(); err != nil || len(rows) != 0 {
		t.Fatal("deleted data resurrected")
	}
}

// A name outlives the last-heard time recorded beside it: a lease that
// announced one and a neighbour entry that never confirmed an answer leave one
// real time and one empty one, and empty must read back as "never" rather than
// as year one.
func TestSeenKeepsANameWithoutASighting(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now().UTC()
	if err := s.SaveSeen([]Seen{{MAC: "aa:bb:cc:dd:ee:ff", Hostname: "phone", HostnameAt: now}}); err != nil {
		t.Fatal(err)
	}
	rows, err := s.Seen()
	if err != nil || len(rows) != 1 {
		t.Fatalf("seen: %+v %v", rows, err)
	}
	if !rows[0].At.IsZero() {
		t.Fatalf("a name was read back as a sighting: %v", rows[0].At)
	}
	if rows[0].Hostname != "phone" || !rows[0].HostnameAt.Equal(now) {
		t.Fatalf("remembered name: %+v", rows[0])
	}
}

func TestRejectCorruptAndFutureDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	if err := os.WriteFile(path, []byte("not sqlite"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("corrupt database accepted")
	}
	path = filepath.Join(t.TempDir(), "runtime.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("PRAGMA user_version=5"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if _, err := Open(path); err == nil {
		t.Fatal("future schema accepted")
	}
}
