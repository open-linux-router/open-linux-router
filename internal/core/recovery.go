package core

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type recoveryKey struct{}
type recoveryTrialKey struct{}

const RecoveryHeader = "X-OLR-Recovery-ID"

// Recovery journals a connectivity trial before the first change. All state
// transitions run under the global apply lock; the mutex only protects readers.
type Recovery struct {
	Store    *Store
	Lock     *Lock
	Path     string
	Timeout  time.Duration
	Capture  func() (json.RawMessage, error)
	Rollback func(context.Context, Document, Document, json.RawMessage) error
	mu       sync.Mutex
	trial    *recoveryRecord
}

type RecoveryView struct {
	ID       string    `json:"id,omitempty"`
	Deadline time.Time `json:"deadline,omitempty"`
	State    string    `json:"state"`
	Error    string    `json:"error,omitempty"`
}

type RecoveryRequest struct {
	ID string `json:"id"`
}

type recoveryRecord struct {
	RecoveryView
	Before  json.RawMessage `json:"before"`
	From    json.RawMessage `json:"from,omitempty"`
	Runtime json.RawMessage `json:"runtime,omitempty"`
}

// ProtectConnectivity is called under the apply lock, after validation and
// preview gates, before saving intent or touching runtime state.
func ProtectConnectivity(ctx context.Context, needed bool) error {
	r, _ := ctx.Value(recoveryKey{}).(*Recovery)
	if r == nil || !needed {
		return nil
	}
	if id, _ := ctx.Value(recoveryTrialKey{}).(string); id != "" {
		v := r.View()
		if v.ID == id && v.State == "pending" && time.Now().Before(v.Deadline) {
			return nil
		}
	}
	return r.Begin()
}

func (r *Recovery) View() RecoveryView {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.trial == nil {
		return RecoveryView{State: "idle"}
	}
	return r.trial.RecoveryView
}

func (r *Recovery) persist(t *recoveryRecord) error {
	data, err := json.Marshal(t)
	if err != nil {
		return err
	}
	return WriteFileAtomic(r.Path, data, 0o600)
}

func (r *Recovery) Begin() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.trial != nil {
		return errors.New("a connectivity change is awaiting confirmation or recovery")
	}
	doc, err := r.Store.Load()
	if err != nil {
		return err
	}
	before, err := r.Store.encode(doc)
	if err != nil {
		return err
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	timeout := r.Timeout
	if timeout == 0 {
		timeout = 2 * time.Minute
	}
	t := &recoveryRecord{RecoveryView: RecoveryView{ID: hex.EncodeToString(nonce[:]), Deadline: time.Now().Add(timeout), State: "pending"}, Before: before}
	if r.Capture != nil {
		t.Runtime, err = r.Capture()
		if err != nil {
			return fmt.Errorf("capturing connectivity recovery point: %w", err)
		}
	}
	if err := r.persist(t); err != nil {
		return fmt.Errorf("saving connectivity recovery point: %w", err)
	}
	r.trial = t
	return nil
}

// Load must run before normal startup reconciliation. A restart during an
// unconfirmed trial always restores the previous configuration immediately.
func (r *Recovery) Load(ctx context.Context) (loadErr error) {
	defer func() {
		if loadErr != nil {
			r.mu.Lock()
			if r.trial == nil {
				r.trial = &recoveryRecord{RecoveryView: RecoveryView{State: "failed", Error: "Cannot read recovery journal; repair it from the console: " + loadErr.Error()}}
			}
			r.mu.Unlock()
		}
	}()
	data, err := os.ReadFile(r.Path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var t recoveryRecord
	if err := json.Unmarshal(data, &t); err != nil {
		return err
	}
	if t.ID == "" || len(t.Before) == 0 {
		return errors.New("invalid connectivity recovery journal")
	}
	if _, err := r.Store.decode(t.Before); err != nil {
		return err
	}
	r.mu.Lock()
	r.trial = &t
	r.mu.Unlock()
	return r.Lock.Do(ctx, func() error { return r.revert(ctx) })
}

func (r *Recovery) Run(ctx context.Context, report func(error)) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			view := r.View()
			if view.State == "idle" || view.ID == "" || time.Now().Before(view.Deadline) {
				continue
			}
			err := r.Lock.Do(ctx, func() error {
				view := r.View()
				if view.State == "idle" || time.Now().Before(view.Deadline) {
					return nil
				}
				return r.revert(ctx)
			})
			if err != nil && ctx.Err() == nil && report != nil {
				report(err)
			}
		}
	}
}

func (r *Recovery) clear() error {
	if err := os.Remove(r.Path); err != nil && !os.IsNotExist(err) {
		return err
	}
	// Persist journal deletion before acknowledging confirmation.
	if err := syncDirectory(r.Path); err != nil {
		return err
	}
	r.mu.Lock()
	r.trial = nil
	r.mu.Unlock()
	return nil
}

func (r *Recovery) revert(ctx context.Context) error {
	r.mu.Lock()
	if r.trial == nil {
		r.mu.Unlock()
		return nil
	}
	t := *r.trial
	r.mu.Unlock()
	before, err := r.Store.decode(t.Before)
	if err != nil {
		return err
	}
	if len(t.From) == 0 {
		current, err := r.Store.Load()
		if err != nil {
			return err
		}
		t.From, err = r.Store.encode(current)
		if err != nil {
			return err
		}
	}
	from, err := r.Store.decode(t.From)
	if err != nil {
		return err
	}
	t.State, t.Error = "recovering", ""
	if err := r.persist(&t); err != nil {
		return err
	}
	r.mu.Lock()
	r.trial = &t
	r.mu.Unlock()
	// Retain the original trial intent for cleanup on every retry. Reading only
	// the restored file would forget addresses/routes created by the trial.
	if r.Rollback == nil {
		err = errors.New("connectivity recovery is unavailable")
	} else {
		err = r.Rollback(ctx, from, before, t.Runtime)
	}
	if err == nil {
		return r.clear()
	}
	t.State, t.Error = "failed", err.Error()
	t.Deadline = time.Now().Add(10 * time.Second)
	r.mu.Lock()
	r.trial = &t
	r.mu.Unlock()
	return errors.Join(err, r.persist(&t))
}

func syncDirectory(path string) error {
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func (r *Recovery) Wrap(rt Route) Route {
	next := rt.Handler
	rt.Handler = func(w http.ResponseWriter, req *http.Request) {
		ctx := context.WithValue(req.Context(), recoveryKey{}, r)
		ctx = context.WithValue(ctx, recoveryTrialKey{}, req.Header.Get(RecoveryHeader))
		if rt.Mutating && rt.Path != "/confirm" && rt.Path != "/revert" && rt.Path != "/access/login" && rt.Path != "/access/logout" {
			ctx = WithMutationGuard(ctx, func() error {
				v := r.View()
				if v.State != "idle" && !(v.State == "pending" && time.Now().Before(v.Deadline) && req.Header.Get(RecoveryHeader) == v.ID) {
					return errors.New("confirm or revert the pending connectivity change before editing configuration")
				}
				return nil
			})
		}
		v := r.View()
		if rt.Mutating && (v.State == "idle" || req.Header.Get(RecoveryHeader) == v.ID) {
			w = &recoveryResponse{ResponseWriter: w, recovery: r}
		}
		next(w, req.WithContext(ctx))
	}
	return rt
}

type recoveryResponse struct {
	http.ResponseWriter
	recovery *Recovery
}

func (w *recoveryResponse) WriteHeader(code int) {
	if v := w.recovery.View(); v.State == "pending" {
		w.Header().Set(RecoveryHeader, v.ID)
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *recoveryResponse) Write(data []byte) (int, error) {
	if v := w.recovery.View(); v.State == "pending" {
		w.Header().Set(RecoveryHeader, v.ID)
	}
	return w.ResponseWriter.Write(data)
}

func (w *recoveryResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (r *Recovery) Routes() []Route {
	return []Route{
		{Method: "GET", Path: "/status", Summary: "Show the pending connectivity change and confirmation deadline.", Tool: "status", Handler: func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			WriteJSON(w, 200, r.View())
		}},
		{Method: "POST", Path: "/confirm", Summary: "Keep a connectivity change after verifying access to the router.", Body: BodyFull, Mutating: true, Handler: r.finish(false)},
		{Method: "POST", Path: "/revert", Summary: "Restore the configuration before the pending connectivity change.", Body: BodyFull, Mutating: true, Handler: r.finish(true)},
	}
}

func (r *Recovery) finish(revert bool) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		var body RecoveryRequest
		if err := DecodeJSON(w, req, &body); err != nil {
			WriteError(w, 400, err.Error())
			return
		}
		err := r.Lock.Do(req.Context(), func() error {
			v := r.View()
			if body.ID == "" || v.ID != body.ID {
				return errors.New("this connectivity change is no longer pending")
			}
			if revert {
				return r.revert(req.Context())
			}
			if v.State != "pending" || !time.Now().Before(v.Deadline) {
				return errors.New("the confirmation deadline has passed; recovery is required")
			}
			return r.clear()
		})
		if err != nil {
			WriteError(w, 409, err.Error())
			return
		}
		WriteJSON(w, 200, r.View())
	}
}
