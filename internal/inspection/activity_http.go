package inspection

import (
	"net/http"
	"net/netip"
	"strconv"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// activityResponse includes the actual bucket width; longer history ranges are
// aggregated for display without changing their byte totals.
type activityResponse struct {
	Buckets   []ActivityBucket `json:"buckets"`
	Since     *time.Time       `json:"since,omitempty"`
	WindowSec int              `json:"window_seconds"`
	BucketSec int              `json:"bucket_seconds"`
	Sampled   int              `json:"sample_seconds"`
	Recording bool             `json:"recording"`
}

// activity supports retained MAC history, or the existing address live view.
func (s *Service) activity(w http.ResponseWriter, r *http.Request) {
	if s.Activity == nil {
		core.WriteError(w, http.StatusServiceUnavailable, "this olrd was built without per-site accounting")
		return
	}
	query := r.URL.Query()
	if mac := query.Get("mac"); mac != "" {
		if _, err := core.NormalizeMAC(mac); err != nil {
			core.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		window, err := parseActivityRange(query.Get("range"))
		if err != nil {
			core.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		out, err := s.Activity.History(mac, window)
		if err != nil {
			core.WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		core.WriteJSON(w, http.StatusOK, out)
		return
	}
	if query.Get("range") != "" {
		core.WriteError(w, http.StatusBadRequest, "a MAC is required for retained activity ranges")
		return
	}
	var addresses []netip.Addr
	for _, raw := range query["address"] {
		addr, err := netip.ParseAddr(raw)
		if err != nil {
			core.WriteError(w, http.StatusBadRequest, "address must be an IP address, not "+strconv.Quote(raw))
			return
		}
		addresses = append(addresses, addr.Unmap())
	}
	if len(addresses) == 0 {
		core.WriteError(w, http.StatusBadRequest, "a MAC or at least one address is required")
		return
	}
	window := activityWindow(0)
	out := activityResponse{Buckets: s.Activity.Snapshot(addresses)}
	if started := s.Activity.Started(); !started.IsZero() {
		now := s.Activity.now()
		window = activityWindow(now.Sub(started))
		since := started
		if edge := now.Add(-window); edge.After(since) {
			since = edge
		}
		out.Since = &since
		out.Recording = true
	}
	out.WindowSec = int(window.Seconds())
	out.BucketSec = int(activityWidth.Seconds())
	out.Sampled = int(SampleInterval.Seconds())
	core.WriteJSON(w, http.StatusOK, out)
}

func parseActivityRange(raw string) (time.Duration, error) {
	if raw == "" {
		return 4 * time.Hour, nil
	}
	switch raw {
	case "7d":
		return 7 * 24 * time.Hour, nil
	case "30d":
		return activityRetention, nil
	}
	window, err := time.ParseDuration(raw)
	if err != nil || window < time.Minute || window > activityRetention {
		return 0, &activityRangeError{}
	}
	return window, nil
}

type activityRangeError struct{}

func (*activityRangeError) Error() string {
	return "activity range must be a duration from 1 minute to 30 days (4h, 24h, 7d, or 30d)"
}
