package inspection

import (
	"net/http"
	"net/netip"
	"strconv"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// activityResponse is the device page's chart: the session's buckets plus the
// facts a reader needs to interpret them.
type activityResponse struct {
	Buckets   []ActivityBucket `json:"buckets"`
	Since     *time.Time       `json:"since,omitempty"`
	WindowSec int              `json:"window_seconds"`
	BucketSec int              `json:"bucket_seconds"`
	Sampled   int              `json:"sample_seconds"`
}

// activity serves GET /api/inspection/activity?address=…
//
// Addresses rather than a MAC: the sampler sees addresses, the device page
// already has them, and a device that dials in over WireGuard has nothing else.
// The series only exists while a session is running; without one the response
// is empty rather than an error, because "inspection is off" is not a failure.
func (s *Service) activity(w http.ResponseWriter, r *http.Request) {
	if s.Activity == nil {
		core.WriteError(w, http.StatusServiceUnavailable, "this olrd was built without per-site accounting")
		return
	}
	query := r.URL.Query()
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
		core.WriteError(w, http.StatusBadRequest, "at least one address is required")
		return
	}

	window := activityWindow(0)
	out := activityResponse{Buckets: s.Activity.Snapshot(addresses)}
	if started := s.Activity.Started(); !started.IsZero() {
		now := time.Now()
		window = activityWindow(now.Sub(started))
		// The axis starts when the switch did, until the session is longer
		// than the ring; then it scrolls with the newest four hours.
		since := started
		if edge := now.Add(-window); edge.After(since) {
			since = edge
		}
		out.Since = &since
	}
	out.WindowSec = int(window.Seconds())
	out.BucketSec = int(activityWidth.Seconds())
	out.Sampled = int(SampleInterval.Seconds())
	core.WriteJSON(w, http.StatusOK, out)
}
