package core

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// ConfigETag identifies intent, never runtime status. It remains stable across
// restarts and changes whenever the stored configuration changes.
func ConfigETag(config any) string {
	data, err := json.Marshal(config)
	if err != nil {
		panic(fmt.Sprintf("core: encoding config revision: %v", err))
	}
	return fmt.Sprintf(`"%x"`, sha256.Sum256(data))
}

// WriteConfig serves intent with its revision. A redacted representation may
// be supplied without losing detection of a concurrent credential change.
func WriteConfig(w http.ResponseWriter, config any, redacted ...any) {
	w.Header().Set("ETag", ConfigETag(config))
	w.Header().Set("Cache-Control", "no-store")
	if len(redacted) > 0 {
		WriteJSON(w, http.StatusOK, redacted[0])
		return
	}
	WriteJSON(w, http.StatusOK, config)
}

// MatchConfig must run under the apply lock, before any write. Older clients
// may omit If-Match; clients editing a fetched document send its strong ETag.
// A rejected edit is never retried against a newer revision automatically.
func MatchConfig(w http.ResponseWriter, r *http.Request, current any) bool {
	if ConfigMatches(r, current) {
		return true
	}
	WriteError(w, http.StatusPreconditionFailed,
		"configuration changed while you were editing; reload it and review your change before saving again")
	return false
}

func ConfigMatches(r *http.Request, current any) bool {
	match := r.Header.Get("If-Match")
	if match == "" {
		return true
	}
	want := ConfigETag(current)
	for _, tag := range strings.Split(match, ",") {
		if tag = strings.TrimSpace(tag); tag == want || tag == "*" {
			return true
		}
	}
	return false
}

// MutateConfig keeps load, edit, cross-module validation and apply in one
// critical section. Request bodies are decoded before this call so a slow
// upload cannot hold the global apply lock.
func MutateConfig[T any](w http.ResponseWriter, r *http.Request, lock *Lock,
	load func() (T, error), edit func(T) (T, error), apply func(T)) {
	if err := lock.Do(r.Context(), func() error {
		current, err := load()
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return nil
		}
		if !MatchConfig(w, r, current) {
			return nil
		}
		desired, err := edit(current)
		if err != nil {
			WriteError(w, http.StatusBadRequest, err.Error())
			return nil
		}
		apply(desired)
		return nil
	}); err != nil {
		WriteError(w, http.StatusServiceUnavailable, "waiting for the apply lock: "+err.Error())
	}
}

func PatchConfig[T any](current T, patch []byte, decode func([]byte) (T, error)) (T, error) {
	data, err := json.Marshal(current)
	if err == nil {
		data, err = MergePatch(data, patch)
	}
	if err != nil {
		var zero T
		return zero, err
	}
	return decode(data)
}
