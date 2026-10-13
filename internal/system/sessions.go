package system

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

const sessionCookie = "olr_session"
const sessionLifetime = 90 * 24 * time.Hour

type session struct {
	Credential string
	Expires    time.Time
}
type attempt struct {
	count int
	until time.Time
}

// Sessions stores token hashes, never the browser's bearer credentials.
type Sessions struct {
	mu       sync.Mutex
	sessions map[[32]byte]session
	attempts map[string]attempt
	work     chan struct{}
	Now      func() time.Time
	path     string
}

// OpenSessions restores browser sessions across daemon and router restarts.
func OpenSessions(path string) (*Sessions, error) {
	s := NewSessions()
	s.path = path
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var entries []storedSession
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.Session.Expires.After(s.now()) {
			s.sessions[entry.Key] = entry.Session
		}
	}
	return s, nil
}

type storedSession struct {
	Key     [32]byte
	Session session
}

// save runs under mu; atomic replacement prevents partially written state.
func (s *Sessions) save() error {
	if s.path == "" {
		return nil
	}
	entries := make([]storedSession, 0, len(s.sessions))
	for key, entry := range s.sessions {
		if entry.Expires.After(s.now()) {
			entries = append(entries, storedSession{key, entry})
		}
	}
	data, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	return core.WriteFileAtomic(s.path, data, 0o600)
}

func NewSessions() *Sessions {
	return &Sessions{sessions: make(map[[32]byte]session), attempts: make(map[string]attempt), work: make(chan struct{}, 2)}
}

func (s *Sessions) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Begin bounds both per-source guessing and global expensive hash work. Source
// addresses come from the connection, never untrusted forwarding headers.
func (s *Sessions) Begin(r *http.Request) (func(), bool) {
	s.mu.Lock()
	now := s.now()
	for ip, entry := range s.attempts {
		if !entry.until.After(now) {
			delete(s.attempts, ip)
		}
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	entry := s.attempts[ip]
	if entry.count >= 5 || (entry.count == 0 && len(s.attempts) >= 1024) {
		s.mu.Unlock()
		return nil, false
	}
	select {
	case s.work <- struct{}{}:
	default:
		s.mu.Unlock()
		return nil, false
	}
	if entry.count == 0 {
		entry.until = now.Add(time.Minute)
	}
	entry.count++
	s.attempts[ip] = entry
	s.mu.Unlock()
	return func() { <-s.work }, true
}

func (s *Sessions) Valid(r *http.Request, password *Password) bool {
	if s == nil || password == nil {
		return false
	}
	cookie, err := r.Cookie(sessionCookie)
	if err != nil || len(cookie.Value) != 43 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := sha256.Sum256([]byte(cookie.Value))
	entry, ok := s.sessions[key]
	if !ok {
		return false
	}
	if !entry.Expires.After(s.now()) || entry.Credential != password.Hash {
		delete(s.sessions, key)
		return false
	}
	return true
}

func (s *Sessions) Issue(w http.ResponseWriter, r *http.Request, password *Password) error {
	if s == nil || password == nil {
		return errors.New("password sessions are unavailable")
	}
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return err
	}
	value := base64.RawURLEncoding.EncodeToString(raw[:])
	s.mu.Lock()
	now := s.now()
	for key, entry := range s.sessions {
		if !entry.Expires.After(now) {
			delete(s.sessions, key)
		}
	}
	if len(s.sessions) >= 256 {
		s.mu.Unlock()
		return errors.New("too many active sessions; sign out another browser or wait for expiry")
	}
	// Replace the caller's previous session instead of leaking it until expiry.
	if old, err := r.Cookie(sessionCookie); err == nil {
		delete(s.sessions, sha256.Sum256([]byte(old.Value)))
	}
	key := sha256.Sum256([]byte(value))
	s.sessions[key] = session{Credential: password.Hash, Expires: now.Add(sessionLifetime)}
	if err := s.save(); err != nil {
		delete(s.sessions, key)
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: value, Path: "/api", HttpOnly: true,
		Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: int(sessionLifetime.Seconds())})
	w.Header().Set("Cache-Control", "no-store")
	return nil
}

func (s *Sessions) Logout(w http.ResponseWriter, r *http.Request) error {
	if s != nil {
		s.mu.Lock()
		if cookie, err := r.Cookie(sessionCookie); err == nil {
			delete(s.sessions, sha256.Sum256([]byte(cookie.Value)))
		}
		err := s.save()
		s.mu.Unlock()
		if err != nil {
			return err
		}
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/api", HttpOnly: true, Secure: r.TLS != nil,
		SameSite: http.SameSiteStrictMode, MaxAge: -1})
	w.Header().Set("Cache-Control", "no-store")
	return nil
}

func (s *Sessions) RevokeAll() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	clear(s.sessions)
	err := s.save()
	s.mu.Unlock()
	return err
}
