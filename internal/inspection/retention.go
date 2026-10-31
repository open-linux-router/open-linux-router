package inspection

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

type retentionPolicy struct {
	Days     int   `json:"days"`
	MaxBytes int64 `json:"max_bytes"`
}

func (s *Service) retention(w http.ResponseWriter, r *http.Request) {
	policy := retentionPolicy{Days: 30, MaxBytes: 5 << 30}
	if r.Method == http.MethodGet {
		data, err := os.ReadFile(filepath.Join(s.Dir, "retention.json"))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			core.WriteError(w, http.StatusInternalServerError, "cannot read archive retention")
			return
		}
		if err == nil && json.Unmarshal(data, &policy) != nil {
			core.WriteError(w, http.StatusInternalServerError, "invalid archive retention setting")
			return
		}
	} else {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&policy); err != nil {
			core.WriteError(w, http.StatusBadRequest, "expected retention days and max_bytes")
			return
		}
		if policy.Days < 1 || policy.Days > 365 || policy.MaxBytes < 1<<20 || policy.MaxBytes > 1<<40 {
			core.WriteError(w, http.StatusBadRequest, "retention must be 1 to 365 days and 1 MiB to 1 TiB")
			return
		}
		if err := os.MkdirAll(s.Dir, 0700); err != nil {
			core.WriteError(w, http.StatusInternalServerError, "cannot save archive retention")
			return
		}
		data, _ := json.Marshal(policy)
		if err := core.WriteFileAtomic(filepath.Join(s.Dir, "retention.json"), data, 0600); err != nil {
			core.WriteError(w, http.StatusInternalServerError, "cannot save archive retention")
			return
		}
	}
	core.WriteJSON(w, http.StatusOK, policy)
}
