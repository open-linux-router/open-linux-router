// Package runtimedb owns OLR's runtime state and bounded observation history.
// Operator intent remains in olr.json; database transactions never apply it.
package runtimedb

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	_ "modernc.org/sqlite"
)

const Path = "/var/lib/open-linux-router/runtime.db"

type Store struct{ db *sql.DB }

type LegacyError struct {
	Name string
	Err  error
}

func (e *LegacyError) Error() string { return fmt.Sprintf("importing legacy %s: %v", e.Name, e.Err) }
func (e *LegacyError) Unwrap() error { return e.Err }

// Open uses a single connection so connection-local pragmas cover every write.
// WAL permits external analysis readers while the daemon commits checkpoints.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = f.Chmod(0600); err != nil {
		f.Close()
		return nil, err
	}
	if err = f.Close(); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err = s.initialize(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) initialize() error {
	for _, pragma := range []string{"PRAGMA busy_timeout=5000", "PRAGMA journal_mode=WAL", "PRAGMA synchronous=FULL", "PRAGMA foreign_keys=ON"} {
		if _, err := s.db.Exec(pragma); err != nil {
			return err
		}
	}
	var result string
	if err := s.db.QueryRow("PRAGMA quick_check").Scan(&result); err != nil {
		return err
	}
	if result != "ok" {
		return fmt.Errorf("runtime database integrity: %s", result)
	}
	return s.transaction(func(tx *sql.Tx) error {
		var version int
		if err := tx.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
			return err
		}
		if version > 3 {
			return fmt.Errorf("unsupported runtime database version %d", version)
		}
		if version == 3 {
			return nil
		}
		if version == 2 {
			// A device that is gone has no source to ask for its name, so the
			// record keeps the last one it announced beside the time it was
			// last heard. Both are observations with a time, never intent.
			_, err := tx.Exec(`ALTER TABLE device_seen ADD COLUMN hostname TEXT NOT NULL DEFAULT ''; ALTER TABLE device_seen ADD COLUMN hostname_at TEXT NOT NULL DEFAULT ''; PRAGMA user_version=3;`)
			return err
		}
		if version == 1 {
			_, err := tx.Exec(`CREATE TABLE geoip_cache (ip TEXT PRIMARY KEY, data BLOB NOT NULL, fetched INTEGER NOT NULL, used INTEGER NOT NULL); CREATE INDEX geoip_cache_used ON geoip_cache(used); ALTER TABLE device_seen ADD COLUMN hostname TEXT NOT NULL DEFAULT ''; ALTER TABLE device_seen ADD COLUMN hostname_at TEXT NOT NULL DEFAULT ''; PRAGMA user_version=3;`)
			return err
		}
		_, err := tx.Exec(`
CREATE TABLE imports (name TEXT PRIMARY KEY);
CREATE TABLE activity (bucket INTEGER NOT NULL, address TEXT NOT NULL, up TEXT NOT NULL, down TEXT NOT NULL, PRIMARY KEY(bucket,address));
CREATE INDEX activity_address_time ON activity(address,bucket);
CREATE TABLE device_seen (mac TEXT PRIMARY KEY, seen_at TEXT NOT NULL, hostname TEXT NOT NULL DEFAULT '', hostname_at TEXT NOT NULL DEFAULT '');
CREATE TABLE sessions (token_hash BLOB PRIMARY KEY CHECK(length(token_hash)=32), credential TEXT NOT NULL, expires_at TEXT NOT NULL);
CREATE TABLE qos_state (boot_id TEXT PRIMARY KEY, state BLOB NOT NULL);
CREATE TABLE recovery_state (name TEXT PRIMARY KEY, state BLOB NOT NULL);
CREATE TABLE geoip_cache (ip TEXT PRIMARY KEY, data BLOB NOT NULL, fetched INTEGER NOT NULL, used INTEGER NOT NULL);
CREATE INDEX geoip_cache_used ON geoip_cache(used);
PRAGMA user_version=3;`)
		return err
	})
}

// Recovery records are safety-critical ownership/original-value snapshots,
// rather than analytics. Callers must refuse destructive changes on read errors.
func (s *Store) Recovery(name string) ([]byte, error) {
	var data []byte
	err := s.db.QueryRow("SELECT state FROM recovery_state WHERE name=?", name).Scan(&data)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return data, err
}
func (s *Store) SaveRecovery(name string, data []byte) error {
	_, err := s.db.Exec("INSERT OR REPLACE INTO recovery_state(name,state) VALUES(?,?)", name, data)
	return err
}
func (s *Store) ImportRecovery(name, path string) error {
	return s.Import(name, path, func(data []byte, tx *sql.Tx) error {
		_, err := tx.Exec("INSERT OR IGNORE INTO recovery_state(name,state) VALUES(?,?)", name, data)
		return err
	})
}
func (s *Store) DeleteRecovery(name string) error {
	_, err := s.db.Exec("DELETE FROM recovery_state WHERE name=?", name)
	return err
}

func (s *Store) transaction(fn func(*sql.Tx) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// Import runs at most once per dataset. Decoder failure leaves both the marker
// and data unchanged; the caller may warn and explicitly skip a corrupt source.
// Legacy files are retained for recovery and are never consulted after commit.
func (s *Store) Import(name, path string, decode func([]byte, *sql.Tx) error) error {
	return s.transaction(func(tx *sql.Tx) error {
		var count int
		if err := tx.QueryRow("SELECT count(*) FROM imports WHERE name=?", name).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if err == nil {
			if err := os.Chmod(path, 0600); err != nil {
				return err
			}
			if err := decode(data, tx); err != nil {
				return &LegacyError{Name: name, Err: err}
			}
		}
		_, err = tx.Exec("INSERT INTO imports(name) VALUES(?)", name)
		return err
	})
}

func (s *Store) SkipImport(name string) error {
	_, err := s.db.Exec("INSERT OR IGNORE INTO imports(name) VALUES(?)", name)
	return err
}

type Activity struct {
	Bucket   int64
	Address  string
	Up, Down uint64
}

// ActivityLimit bounds one history read, and with it what a restore can retain.
//
// It lives here rather than in gateway because the query has to fetch one row
// more than the limit to notice that the limit was exceeded, and because a
// history that was checkpointed has to be readable back: the sampler applies
// the same ceiling, so the two cannot disagree about what fits.
const ActivityLimit = 250_000

// Counters use decimal text to preserve Go's full uint64 range in SQLite.
func PutActivity(tx *sql.Tx, rows []Activity) error {
	stmt, err := tx.Prepare("INSERT OR REPLACE INTO activity(bucket,address,up,down) VALUES(?,?,?,?)")
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, r := range rows {
		if _, err := stmt.Exec(r.Bucket, r.Address, fmt.Sprint(r.Up), fmt.Sprint(r.Down)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) SaveActivity(rows []Activity, cutoff int64) error {
	return s.transaction(func(tx *sql.Tx) error {
		if _, err := tx.Exec("DELETE FROM activity WHERE bucket < ? OR bucket > ?", cutoff, time.Now().Unix()); err != nil {
			return err
		}
		return PutActivity(tx, rows)
	})
}

func (s *Store) Activity(cutoff, now int64) ([]Activity, error) {
	rows, err := s.db.Query("SELECT bucket,address,up,down FROM activity WHERE bucket>=? AND bucket<=? ORDER BY bucket,address LIMIT ?", cutoff, now, ActivityLimit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Activity{}
	for rows.Next() {
		var r Activity
		var up, down string
		if err := rows.Scan(&r.Bucket, &r.Address, &up, &down); err != nil {
			return nil, err
		}
		r.Up, err = strconv.ParseUint(up, 10, 64)
		if err != nil {
			return nil, err
		}
		r.Down, err = strconv.ParseUint(down, 10, 64)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if len(out) > ActivityLimit {
		return nil, fmt.Errorf("activity history exceeds entry limit")
	}
	return out, rows.Err()
}

// Seen is what the record keeps about one device that is no longer here: when
// it was last heard from, and the last name it announced and when.
//
// Two observations with two times, in one row because they are forgotten
// together — retaining a name for a MAC nobody has seen in months would be
// history that outlives the device it describes. Either time may be zero: a
// device can hold a lease (and a name) that the neighbour table never confirmed
// answering, and an existing row from before hostnames were kept has no name.
type Seen struct {
	MAC string
	At  time.Time

	Hostname   string
	HostnameAt time.Time
}

// seenTime formats a timestamp for storage, and the zero time as empty rather
// than as year one: it keeps "never" distinguishable from a real instant.
func seenTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func parseSeenTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339Nano, s)
}

func PutSeen(tx *sql.Tx, rows []Seen) error {
	for _, r := range rows {
		if _, err := tx.Exec("INSERT OR REPLACE INTO device_seen(mac,seen_at,hostname,hostname_at) VALUES(?,?,?,?)",
			r.MAC, seenTime(r.At), r.Hostname, seenTime(r.HostnameAt)); err != nil {
			return err
		}
	}
	return nil
}
func (s *Store) SaveSeen(rows []Seen) error {
	return s.transaction(func(tx *sql.Tx) error {
		if _, err := tx.Exec("DELETE FROM device_seen"); err != nil {
			return err
		}
		return PutSeen(tx, rows)
	})
}
func (s *Store) Seen() ([]Seen, error) {
	rows, err := s.db.Query("SELECT mac,seen_at,hostname,hostname_at FROM device_seen")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Seen{}
	for rows.Next() {
		var r Seen
		var at, named string
		if err := rows.Scan(&r.MAC, &at, &r.Hostname, &named); err != nil {
			return nil, err
		}
		if r.At, err = parseSeenTime(at); err != nil {
			return nil, err
		}
		if r.HostnameAt, err = parseSeenTime(named); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

type Session struct {
	Key        [32]byte
	Credential string
	Expires    time.Time
}

func PutSessions(tx *sql.Tx, rows []Session) error {
	for _, r := range rows {
		if _, err := tx.Exec("INSERT OR REPLACE INTO sessions(token_hash,credential,expires_at) VALUES(?,?,?)", r.Key[:], r.Credential, r.Expires.UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
	}
	return nil
}
func (s *Store) SaveSessions(rows []Session) error {
	return s.transaction(func(tx *sql.Tx) error {
		if _, err := tx.Exec("DELETE FROM sessions"); err != nil {
			return err
		}
		return PutSessions(tx, rows)
	})
}
func (s *Store) Sessions() ([]Session, error) {
	rows, err := s.db.Query("SELECT token_hash,credential,expires_at FROM sessions")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Session{}
	for rows.Next() {
		var r Session
		var key []byte
		var at string
		if err := rows.Scan(&key, &r.Credential, &at); err != nil {
			return nil, err
		}
		if len(key) != 32 {
			return nil, fmt.Errorf("invalid session hash")
		}
		copy(r.Key[:], key)
		r.Expires, err = time.Parse(time.RFC3339Nano, at)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func PutQoS(tx *sql.Tx, boot string, data []byte) error {
	if !json.Valid(data) {
		return fmt.Errorf("invalid QoS state")
	}
	_, err := tx.Exec("INSERT OR REPLACE INTO qos_state(boot_id,state) VALUES(?,?)", boot, data)
	return err
}
func (s *Store) SaveQoS(boot string, data []byte) error {
	return s.transaction(func(tx *sql.Tx) error {
		if _, err := tx.Exec("DELETE FROM qos_state WHERE boot_id<>?", boot); err != nil {
			return err
		}
		return PutQoS(tx, boot, data)
	})
}
func (s *Store) QoS(boot string) ([]byte, error) {
	var data []byte
	err := s.db.QueryRow("SELECT state FROM qos_state WHERE boot_id=?", boot).Scan(&data)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return data, err
}
