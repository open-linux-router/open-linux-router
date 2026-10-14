package daemon

import (
	"errors"
	"github.com/open-linux-router/open-linux-router/internal/runtimedb"
	"log/slog"
)

// Only disposable history/session imports may be skipped. Ownership and host
// restoration state always propagate errors before any system modification.
func restoreHistory[T any](db *runtimedb.Store, log *slog.Logger, open func() (T, error)) (T, error) {
	value, err := open()
	var legacy *runtimedb.LegacyError
	if errors.As(err, &legacy) {
		log.Error("skipping invalid legacy runtime history; source retained for recovery", "dataset", legacy.Name, "error", err)
		if skipErr := db.SkipImport(legacy.Name); skipErr != nil {
			return value, skipErr
		}
		return open()
	}
	return value, err
}
