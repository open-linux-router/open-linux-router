package system

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Authenticate protects TCP only; the local control socket keeps its file
// permission authentication and remains the password recovery path.
func Authenticate(a Applier, sessions *Sessions, token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api" && !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		public := r.Method == "GET" && r.URL.Path == "/api/system/access" || r.Method == "POST" && (r.URL.Path == "/api/system/access/claim" || r.URL.Path == "/api/system/access/login" || r.URL.Path == "/api/system/access/logout")
		if public {
			next.ServeHTTP(w, r)
			return
		}
		check := func() error {
			cfg, err := a.Load()
			if err != nil {
				return fmt.Errorf("cannot read administrator access configuration: %w", err)
			}
			if core.ValidBearer(token, r) {
				return nil
			}
			if cfg.RequiresPassword() {
				if sessions.Valid(r, cfg.Access.Password) {
					return nil
				}
				return fmt.Errorf("administrator password required; sign in again")
			}
			if token != "" {
				return fmt.Errorf("API token required")
			}
			return nil
		}
		if err := check(); err != nil {
			if _, loadErr := a.Load(); loadErr != nil {
				core.WriteError(w, 503, "cannot read administrator access configuration")
				return
			}
			core.WriteError(w, 401, err.Error())
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			r = r.WithContext(core.WithMutationGuard(r.Context(), check))
		}
		next.ServeHTTP(w, r)
	})
}
