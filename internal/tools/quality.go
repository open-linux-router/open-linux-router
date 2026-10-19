package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

const (
	qualityVersion = "quality-v3"
	qualityCatalog = "reference-v1"
	qualitySamples = 6
)

type qualityTarget struct {
	ID, Name, URL string
	Status        int
}

var qualityTargets = []qualityTarget{
	{"cloudflare", "Cloudflare", "https://speed.cloudflare.com/__down?bytes=0", http.StatusOK},
	{"google", "Google", "https://www.google.com/generate_204", http.StatusNoContent},
	{"github", "GitHub", "https://api.github.com/zen", http.StatusOK},
}

// QualitySample describes one fresh HTTPS connection, not a browser page load.
// Nullable durations mean that stage did not complete, never a measured zero.
type QualitySample struct {
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at"`
	State       string    `json:"state"`
	Stage       string    `json:"stage"`
	Address     string    `json:"address,omitempty"`
	DNS         *float64  `json:"dns_ms"`
	TCP         *float64  `json:"tcp_ms"`
	Response    *float64  `json:"response_ms"`
	Status      int       `json:"status,omitempty"`
	Error       string    `json:"error,omitempty"`
}

type QualityTargetResult struct {
	ID      string          `json:"id"`
	Name    string          `json:"name"`
	URL     string          `json:"url"`
	Family  string          `json:"family"`
	Samples []QualitySample `json:"samples"`
}

type QualitySnapshot struct {
	Version        string                `json:"scoring_version"`
	Catalog        string                `json:"catalog_version"`
	Profile        string                `json:"profile_id"`
	ProfileVersion string                `json:"profile_version"`
	Scope          string                `json:"scope"`
	RunID          string                `json:"run_id"`
	State          string                `json:"state"`
	Phase          string                `json:"phase"`
	IncludeSpeed   bool                  `json:"include_speed"`
	StartedAt      *time.Time            `json:"started_at"`
	CompletedAt    *time.Time            `json:"completed_at"`
	ExpiresAt      *time.Time            `json:"expires_at"`
	Targets        []QualityTargetResult `json:"targets"`
	Speed          *SpeedResult          `json:"speed"`
	SpeedAt        *time.Time            `json:"speed_at"`
	SpeedError     string                `json:"speed_error,omitempty"`
	NAT            *NATResult            `json:"nat"`
	NATError       string                `json:"nat_error,omitempty"`
	Categories     []QualityCategory     `json:"categories"`
	Overall        QualityOverall        `json:"overall"`
}

type qualityRun struct {
	mu       sync.Mutex
	cancel   context.CancelFunc
	closed   bool
	snapshot QualitySnapshot
}

// RestoreQuality loads saved evidence before serving requests. Starting a new
// assessment leaves this checkpoint intact until that assessment finishes.
func (h *HTTP) RestoreQuality() error {
	if h.QualityPath == "" {
		return nil
	}
	data, err := os.ReadFile(h.QualityPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var snapshot QualitySnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return err
	}
	switch snapshot.State {
	case "completed", "cancelled", "timed_out":
	default:
		return fmt.Errorf("invalid saved quality state %q", snapshot.State)
	}
	h.quality.mu.Lock()
	h.quality.snapshot = snapshot
	h.quality.mu.Unlock()
	return nil
}

// Called under quality.mu so another run cannot overtake this checkpoint.
func (h *HTTP) saveQuality() {
	if h.QualityPath == "" {
		return
	}
	data, err := json.Marshal(h.quality.snapshot)
	if err == nil {
		err = core.WriteFileAtomic(h.QualityPath, data, 0o600)
	}
	if err != nil {
		slog.Error("could not persist quality assessment", "error", err)
	}
}

func (h *HTTP) qualitySnapshot() QualitySnapshot {
	h.quality.mu.Lock()
	snapshot := h.quality.snapshot
	h.quality.mu.Unlock()
	// Published evidence is immutable. Scoring constructs new result slices and
	// does not mutate the evidence shared with concurrent passive readers.
	if snapshot.State == "" {
		snapshot.State = "idle"
	}
	snapshot.Version, snapshot.Catalog, snapshot.Scope = qualityVersion, qualityCatalog, "router-default-route"
	snapshot.Profile, snapshot.ProfileVersion = "reference", "1"
	if snapshot.Targets == nil {
		snapshot.Targets = []QualityTargetResult{}
	}
	snapshot.Categories, snapshot.Overall = scoreQuality(snapshot, time.Now())
	return snapshot
}

func (h *HTTP) getQuality(w http.ResponseWriter, r *http.Request) {
	core.WriteJSON(w, http.StatusOK, h.qualitySnapshot())
}

func (h *HTTP) startQuality(w http.ResponseWriter, r *http.Request) {
	includeSpeed := false
	if value := r.URL.Query().Get("speed"); value != "" {
		var err error
		includeSpeed, err = strconv.ParseBool(value)
		if err != nil {
			core.WriteError(w, http.StatusBadRequest, "speed must be true or false")
			return
		}
	}
	h.quality.mu.Lock()
	if h.quality.closed {
		h.quality.mu.Unlock()
		core.WriteError(w, http.StatusServiceUnavailable, "diagnostics are shutting down")
		return
	}
	// Reserve existing speed/NAT runners for the whole assessment, including
	// the response baseline, to avoid contamination by those diagnostic tests.
	if h.quality.cancel != nil || !h.running.CompareAndSwap(false, true) {
		h.quality.mu.Unlock()
		core.WriteError(w, http.StatusConflict, "a quality assessment or speed test is already running")
		return
	}
	if !h.natRunning.CompareAndSwap(false, true) {
		h.running.Store(false)
		h.quality.mu.Unlock()
		core.WriteError(w, http.StatusConflict, "a NAT test is already running")
		return
	}
	parent := h.QualityContext
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, 150*time.Second)
	now := time.Now().UTC()
	h.quality.cancel = cancel
	h.quality.snapshot = QualitySnapshot{State: "running", Phase: "responses", IncludeSpeed: includeSpeed, RunID: now.Format(time.RFC3339Nano), StartedAt: &now}
	h.quality.mu.Unlock()
	go h.measureQuality(ctx, includeSpeed)
	core.WriteJSON(w, http.StatusAccepted, h.qualitySnapshot())
}

func (h *HTTP) cancelQuality(w http.ResponseWriter, r *http.Request) {
	h.quality.mu.Lock()
	if h.quality.cancel != nil {
		h.quality.cancel()
	}
	h.quality.mu.Unlock()
	core.WriteJSON(w, http.StatusOK, h.qualitySnapshot())
}

func (h *HTTP) measureQuality(ctx context.Context, includeSpeed bool) {
	defer func() {
		h.quality.mu.Lock()
		now := time.Now().UTC()
		h.quality.snapshot.CompletedAt = &now
		h.quality.snapshot.State = "completed"
		if ctx.Err() == context.Canceled {
			h.quality.snapshot.State = "cancelled"
		} else if ctx.Err() != nil {
			h.quality.snapshot.State = "timed_out"
		}
		h.quality.snapshot.Phase = ""
		h.saveQuality()
		h.quality.cancel()
		h.quality.cancel = nil
		h.running.Store(false)
		h.natRunning.Store(false)
		h.quality.mu.Unlock()
	}()
	probe := h.qualityProbe
	if probe == nil {
		probe = probeQuality
	}
	results := make([]QualityTargetResult, len(qualityTargets)*2)
	var wg sync.WaitGroup
	for i, target := range qualityTargets {
		for j, family := range []string{"ipv4", "ipv6"} {
			index := i*2 + j
			wg.Go(func() {
				result := QualityTargetResult{ID: target.ID, Name: target.Name, URL: target.URL, Family: family, Samples: []QualitySample{}}
				for range qualitySamples {
					if ctx.Err() != nil {
						break
					}
					bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
					result.Samples = append(result.Samples, probe(bounded, target, family))
					cancel()
				}
				results[index] = result
			})
		}
	}
	wg.Wait()
	h.quality.mu.Lock()
	h.quality.snapshot.Targets = results
	h.quality.snapshot.Phase = "speed"
	h.quality.mu.Unlock()
	if includeSpeed && ctx.Err() == nil {
		bounded, cancel := context.WithTimeout(ctx, 90*time.Second)
		run := h.Run
		if run == nil {
			run = runCloudflare
		}
		result, err := run(bounded)
		cancel()
		h.quality.mu.Lock()
		if err == nil {
			now := time.Now().UTC()
			h.quality.snapshot.Speed, h.quality.snapshot.SpeedAt = &result, &now
		} else {
			h.quality.snapshot.SpeedError = err.Error()
		}
		h.quality.mu.Unlock()
	}
	if ctx.Err() == nil {
		h.quality.mu.Lock()
		h.quality.snapshot.Phase = "nat"
		h.quality.mu.Unlock()
		bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
		run := h.RunNAT
		if run == nil {
			run = runNAT
		}
		result, err := run(bounded)
		cancel()
		h.quality.mu.Lock()
		if err == nil {
			h.quality.snapshot.NAT = &result
		} else {
			h.quality.snapshot.NATError = err.Error()
		}
		h.quality.mu.Unlock()
	}
}
