package inspection

import (
	"net/http"
	"net/netip"
	"strconv"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// activityResponse is the device page's chart: the retained buckets plus the
// facts a reader needs to interpret them.
type activityResponse struct {
	Buckets   []ActivityBucket `json:"buckets"`
	Days      int              `json:"days"`
	BucketSec int              `json:"bucket_seconds"`
	Retention int              `json:"retention_days"`
	Sampled   int              `json:"sample_seconds"`
}

// activity serves GET /api/inspection/activity?address=…&days=7.
//
// Addresses rather than a MAC: the sampler sees addresses, the device page
// already has them, and a device that dials in over WireGuard has nothing else.
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

	days := 7
	if raw := query.Get("days"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 30 {
			core.WriteError(w, http.StatusBadRequest, "days must be a whole number between 1 and 30")
			return
		}
		days = n
	}

	since := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	core.WriteJSON(w, http.StatusOK, activityResponse{
		Buckets:   s.Activity.Snapshot(addresses, since),
		Days:      days,
		BucketSec: int(activityWidth.Seconds()),
		Retention: int(ActivityRetention.Hours() / 24),
		Sampled:   int(SampleInterval.Seconds()),
	})
}
