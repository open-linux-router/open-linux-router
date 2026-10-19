package remote

import (
	"github.com/open-linux-router/open-linux-router/internal/core"
	"net/http"
	"time"
)

type proxyApplyResponse struct {
	Plan  proxyPlanView   `json:"plan"`
	Steps []Step          `json:"steps,omitempty"`
	Error *core.ErrorBody `json:"error,omitempty"`

	// Config is what is stored now, redacted. Returned on the refusal path
	// especially: it says the document did not move.
	//
	// `any` because the two proxies return different sections and this envelope
	// is shared. It is always the object's own section, never the whole
	// document — a client asking about the proxy is not told about the tunnel.
	Config any `json:"config,omitempty"`
}

// mutateProxy is the one write path the proxy's mutating routes go through.
//
// The same shape as the tunnel's and deliberately not shared with it: the two
// differ in every line that touches a plan, and a common helper would be a
// parameterised thing whose body is two `if`s on which object it was given.
func (h HTTP) mutateProxy(w http.ResponseWriter, r *http.Request, forceConfirm bool, proxy ProxyApplier, edit func(*Config) error) {
	kind := proxy.Object.kind()
	dryRun, confirm, err := gateParams(r)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	confirm = confirm || forceConfirm

	stale := false
	var (
		editErr   error
		invalid   Result
		failed    error
		plan      ProxyPlan
		rendered  Rendered
		result    ProxyApplyResult
		stored    Config
		applyErr  error
		generated bool
		held      bool
	)

	if lockErr := h.Lock.Do(r.Context(), func() error {
		cfg, err := proxy.Load()
		if err != nil {
			failed = err
			return nil
		}
		if !core.MatchConfig(w, r, cfg) {
			stale = true
			return nil
		}
		previous := cfg.Clone()
		if err := edit(&cfg); err != nil {
			editErr = err
			return nil
		}

		if kind.Enabled(cfg) {
			did, err := kind.Fill(&cfg)
			if err != nil {
				failed = err
				return nil
			}
			generated = did
		}
		cfg.Normalize()

		res := kind.Validate(cfg)
		validateEndpoint(&res, cfg)
		if !res.OK() {
			invalid = res
			return nil
		}

		obs, err := proxy.Observe(r.Context())
		if err != nil {
			failed = err
			return nil
		}
		plan, rendered, err = kind.Build(cfg, previous, proxy.Paths, obs)
		if err != nil {
			failed = err
			return nil
		}

		if dryRun || (plan.Impact == ImpactDisruptive && !confirm) {
			held = true
			stored = previous
			return nil
		}

		result, applyErr = proxy.ApplyPlanned(r.Context(), cfg, plan, rendered)
		stored, _ = proxy.Load()
		return nil
	}); lockErr != nil {
		core.WriteError(w, http.StatusServiceUnavailable,
			"timed out waiting for the apply lock: "+lockErr.Error())
		return
	}

	if stale {
		return
	}

	switch {
	case failed != nil:
		core.WriteError(w, http.StatusInternalServerError, failed.Error())
		return
	case editErr != nil:
		core.WriteError(w, statusFor(editErr), editErr.Error())
		return
	case !invalid.OK():
		core.WriteError(w, http.StatusUnprocessableEntity,
			"invalid remote-access configuration", problems(invalid.Errors)...)
		return
	}

	view := viewProxyPlan(plan, generated)
	if dryRun {
		core.WriteJSON(w, http.StatusOK, view)
		return
	}

	redacted := kind.Redacted(stored.Redacted())
	if held {
		core.WriteJSON(w, http.StatusConflict, proxyApplyResponse{
			Plan:   view,
			Config: redacted,
			Error: &core.ErrorBody{Message: joinReasons(plan.Reasons) +
				"; repeat the request with confirm=true to go ahead"},
		})
		return
	}

	if len(result.Steps) > 0 {
		h.Events.Publish(core.Event{Type: core.EventApplied, Module: ModuleName})
	}

	resp := proxyApplyResponse{Plan: view, Steps: result.Steps, Config: redacted}
	if applyErr != nil {
		resp.Error = &core.ErrorBody{Message: applyErr.Error()}
		core.WriteJSON(w, http.StatusInternalServerError, resp)
		return
	}
	core.WriteJSON(w, http.StatusOK, resp)
}

// --- dry run ----------------------------------------------------------------

type linkResponse struct {
	// URL is the whole thing a client needs, in one string.
	URL string `json:"url"`

	// Label is what a client app shows in its list of servers.
	Label string    `json:"label,omitempty"`
	AsOf  time.Time `json:"as_of"`
}
