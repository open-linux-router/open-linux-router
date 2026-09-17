package remote

import (
	"bytes"
	"encoding/json"
	"net/http"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// The proxy's half of the module's surface.
//
// Smaller than the tunnel's, and the difference is the object rather than the
// effort: there are no devices here. One password serves every client, so there
// is nothing to add, nothing to remove and nothing to list — which is also why
// this half has a route the other one cannot have, and lacks one the other one
// needs.
//
//   - **`GET /shadowsocks/link` exists**, and hands over the secret. It can,
//     because there is one secret and olr stores it. The tunnel's equivalent
//     cannot exist at all: a peer's private key is generated, returned once and
//     forgotten.
//   - **There is no `/shadowsocks/clients`.** Modelling a client list that the
//     server cannot tell apart would look exactly like the tunnel's device list
//     and behave nothing like it — removing an entry would revoke nobody. That
//     is the "looks unified, behaves differently" trap, and the honest answer is
//     to have no list.

func (h HTTP) shadowsocksRoutes() []core.Route {
	return []core.Route{
		{
			Method: "GET", Path: "/shadowsocks/config", Tool: "show shadowsocks",
			Summary: "Show the stored proxy configuration: the port, the cipher and whether UDP is carried. " +
				"The password is never returned here.",
			Handler: h.getShadowsocksConfig,
		},
		{
			Method: "PATCH", Path: "/shadowsocks/config",
			Summary: "Change the proxy's settings, or turn it on and off. " +
				"Changing the cipher regenerates the password, so every client has to be given a new link.",
			Body:     core.BodyRelaxed,
			Query:    gate(),
			Mutating: true,
			Handler:  h.patchShadowsocksConfig,
		},
		{
			// **No Tool, and this is the one read route in olr that is
			// deliberately not published to an agent.**
			//
			// The conformance suite's R2 says every read must be a tool, and
			// says an exception has to be argued rather than assumed. This is
			// the argument: the response *is* the credential. Every other read
			// in olr redacts, and publishing a tool whose whole purpose is to
			// return an unredacted secret would put it in a transcript the
			// first time a model was asked how remote access is set up.
			//
			// A human reaches it through `olr remote show shadowsocks link`,
			// which is a thing they chose to type.
			Method: "GET", Path: "/shadowsocks/link",
			Summary: "Return the ss:// link a client imports. This contains the password, " +
				"so unlike every other read here it is not redacted.",
			Handler: h.getShadowsocksLink,
		},
		{
			Method: "POST", Path: "/shadowsocks/plan", Tool: "show shadowsocks plan",
			Summary: "Show what a proxy change would do without doing it. " +
				"An empty body plans the stored configuration, which answers whether the box has drifted.",
			Body:    core.BodyRelaxed,
			Handler: h.postShadowsocksPlan,
		},
		{
			Method: "POST", Path: "/shadowsocks/apply",
			Summary: "Re-render the proxy's configuration and restart it from stored intent, changing no intent. " +
				"This is the repair path for a half-applied change or a file somebody edited.",
			Mutating: true,
			Handler:  h.postShadowsocksApply,
		},
		{
			Method: "GET", Path: "/shadowsocks/status", Tool: "status shadowsocks",
			Summary: "Show whether the proxy is installed and running, and whether the box still matches " +
				"the stored configuration.",
			Handler: h.getShadowsocksStatus,
		},
	}
}

// --- intent -----------------------------------------------------------------

func (h HTTP) getShadowsocksConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.Proxy.Load()
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	core.WriteJSON(w, http.StatusOK, cfg.Redacted().Shadowsocks)
}

func (h HTTP) patchShadowsocksConfig(w http.ResponseWriter, r *http.Request) {
	patch, err := core.ReadBody(w, r)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(bytes.TrimSpace(patch)) == 0 {
		core.WriteError(w, http.StatusBadRequest, "empty patch body")
		return
	}

	h.mutateProxy(w, r, false, func(cfg *Config) error {
		current, err := json.Marshal(cfg.Shadowsocks)
		if err != nil {
			return err
		}
		merged, err := core.MergePatch(current, patch)
		if err != nil {
			return badRequest(err)
		}
		next, err := UnmarshalShadowsocks(merged)
		if err != nil {
			return badRequest(err)
		}
		// The mask means "unchanged", which is the only thing it can honestly
		// mean: a UI that rendered the redacted value into a form and sent it
		// back would otherwise store eight asterisks as the password and take
		// every client offline.
		if next.Password == RedactedSecret {
			next.Password = cfg.Shadowsocks.Password
		}
		cfg.Shadowsocks = next
		return nil
	})
}

func (h HTTP) postShadowsocksApply(w http.ResponseWriter, r *http.Request) {
	// forceConfirm: this re-applies intent the operator stored earlier — and
	// confirmed then, if it needed confirming — so there is no new decision to
	// put to them.
	h.mutateProxy(w, r, true, func(*Config) error { return nil })
}

// proxyApplyResponse always carries the plan and the steps, successful or not
// (§5.3.2: there is no rollback, so a half-finished change stays half-finished
// and the honest thing is to say which steps landed).
type proxyApplyResponse struct {
	Plan  proxyPlanView   `json:"plan"`
	Steps []Step          `json:"steps,omitempty"`
	Error *core.ErrorBody `json:"error,omitempty"`

	// Config is what is stored now, redacted. Returned on the refusal path
	// especially: it says the document did not move.
	Config *Shadowsocks `json:"config,omitempty"`
}

// mutateProxy is the one write path the proxy's mutating routes go through.
//
// The same shape as the tunnel's and deliberately not shared with it: the two
// differ in every line that touches a plan, and a common helper would be a
// parameterised thing whose body is two `if`s on which object it was given.
func (h HTTP) mutateProxy(w http.ResponseWriter, r *http.Request, forceConfirm bool, edit func(*Config) error) {
	dryRun, confirm, err := gateParams(r)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	confirm = confirm || forceConfirm

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
		cfg, err := h.Proxy.Load()
		if err != nil {
			failed = err
			return nil
		}
		previous := cfg.Clone()
		if err := edit(&cfg); err != nil {
			editErr = err
			return nil
		}

		// The password is filled in rather than demanded, and regenerated when
		// the cipher no longer accepts it. That is derived state rather than
		// inferred behaviour: nothing an operator could type would be better
		// than random bytes, and a password that does not fit its cipher is a
		// server that will not start (Cipher.KeyLen).
		if cfg.Shadowsocks.Enabled {
			next, did, err := cfg.Shadowsocks.WithGeneratedPassword()
			if err != nil {
				failed = err
				return nil
			}
			cfg.Shadowsocks, generated = next, did
		}
		cfg.Normalize()

		res := ValidateShadowsocks(cfg)
		validateEndpoint(&res, cfg)
		if !res.OK() {
			invalid = res
			return nil
		}

		obs, err := h.Proxy.Observe(r.Context())
		if err != nil {
			failed = err
			return nil
		}
		plan, rendered, err = BuildProxyPlan(cfg, previous, h.Proxy.Paths, obs)
		if err != nil {
			failed = err
			return nil
		}

		if dryRun || (plan.Impact == ImpactDisruptive && !confirm) {
			held = true
			stored = previous
			return nil
		}

		result, applyErr = h.Proxy.ApplyPlanned(r.Context(), cfg, plan, rendered)
		stored, _ = h.Proxy.Load()
		return nil
	}); lockErr != nil {
		core.WriteError(w, http.StatusServiceUnavailable,
			"timed out waiting for the apply lock: "+lockErr.Error())
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

	redacted := stored.Redacted().Shadowsocks
	if held {
		core.WriteJSON(w, http.StatusConflict, proxyApplyResponse{
			Plan:   view,
			Config: &redacted,
			Error: &core.ErrorBody{Message: joinReasons(plan.Reasons) +
				"; repeat the request with confirm=true to go ahead"},
		})
		return
	}

	if len(result.Steps) > 0 {
		h.Events.Publish(core.Event{Type: core.EventApplied, Module: ModuleName})
	}

	resp := proxyApplyResponse{Plan: view, Steps: result.Steps, Config: &redacted}
	if applyErr != nil {
		resp.Error = &core.ErrorBody{Message: applyErr.Error()}
		core.WriteJSON(w, http.StatusInternalServerError, resp)
		return
	}
	core.WriteJSON(w, http.StatusOK, resp)
}

// --- dry run ----------------------------------------------------------------

func (h HTTP) postShadowsocksPlan(w http.ResponseWriter, r *http.Request) {
	data, err := core.ReadBody(w, r)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	stored, err := h.Proxy.Load()
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	cfg := stored
	if len(bytes.TrimSpace(data)) > 0 {
		next, err := UnmarshalShadowsocks(data)
		if err != nil {
			core.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		if next.Password == RedactedSecret {
			next.Password = stored.Shadowsocks.Password
		}
		cfg = stored.Clone()
		cfg.Shadowsocks = next
	}

	obs, err := h.Proxy.Observe(r.Context())
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	plan, _, err := BuildProxyPlan(cfg, stored, h.Proxy.Paths, obs)
	if err != nil {
		message := err.Error()
		if len(plan.Validation.Errors) > 0 {
			message = "invalid remote-access configuration"
		}
		core.WriteError(w, http.StatusUnprocessableEntity, message,
			problems(plan.Validation.Errors)...)
		return
	}
	core.WriteJSON(w, http.StatusOK, viewProxyPlan(plan, false))
}

// --- the link ---------------------------------------------------------------

type linkResponse struct {
	// URL is the whole thing a client needs, in one string.
	URL string `json:"url"`

	// Label is what a client app shows in its list of servers.
	Label string    `json:"label,omitempty"`
	AsOf  time.Time `json:"as_of"`
}

// getShadowsocksLink hands over the credential, deliberately.
//
// This is the route the redaction everywhere else exists to make safe: a
// password that appears in no `show`, no plan and no log has to appear
// *somewhere*, or the feature cannot be used. Putting it behind its own address
// means seeing it is an act rather than a side effect of reading configuration.
func (h HTTP) getShadowsocksLink(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.Proxy.Load()
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	label := cfg.EndpointHost()
	url, err := ClientURL(cfg, label)
	if err != nil {
		// 409 rather than 500: nothing is broken, the configuration is simply
		// not far enough along to produce a link, and the message says which
		// half is missing.
		core.WriteError(w, http.StatusConflict, err.Error())
		return
	}
	core.WriteJSON(w, http.StatusOK, linkResponse{URL: url, Label: label, AsOf: stamp()})
}

// --- observed ---------------------------------------------------------------

type proxyStatus struct {
	Enabled bool   `json:"enabled"`
	Port    uint16 `json:"listen_port"`
	Cipher  Cipher `json:"cipher"`
	UDP     bool   `json:"udp"`

	// Service is what systemd knows. Absent, with ServiceError set, when the
	// query itself failed — normal on a developer box with no D-Bus, and it
	// must not take the rest of the answer down with it.
	Service      *core.UnitStatus `json:"service,omitempty"`
	ServiceError string           `json:"service_error,omitempty"`

	// Binary is the server olr found, and BinaryError says why it found none.
	Binary      string `json:"binary,omitempty"`
	BinaryError string `json:"binary_error,omitempty"`

	Drifted    bool           `json:"drifted"`
	Drift      *proxyPlanView `json:"drift,omitempty"`
	DriftError string         `json:"drift_error,omitempty"`
	AsOf       time.Time      `json:"as_of"`
}

func (h HTTP) getShadowsocksStatus(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.Proxy.Load()
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	core.WriteJSON(w, http.StatusOK, h.proxyStatusFor(r, cfg))
}

// proxyStatusFor is the proxy's half of health, so that the module-level status
// can report it beside the tunnel's without a second implementation.
func (h HTTP) proxyStatusFor(r *http.Request, cfg Config) proxyStatus {
	resp := proxyStatus{
		AsOf:    stamp(),
		Enabled: cfg.Shadowsocks.Enabled,
		Port:    cfg.Shadowsocks.PortOrDefault(),
		Cipher:  cfg.Shadowsocks.Cipher.OrDefault(),
		UDP:     cfg.Shadowsocks.UDPEnabled(),
	}

	if binary, err := FindShadowsocks(); err != nil {
		resp.BinaryError = ErrShadowsocksMissing().Error()
	} else {
		resp.Binary = binary
	}

	// Each half of health is reported independently and neither can suppress
	// the other (§5.4).
	if h.Proxy.Unit != nil {
		if svc, err := h.Proxy.Unit.Status(r.Context()); err != nil {
			resp.ServiceError = err.Error()
		} else {
			resp.Service = &svc
		}
	}

	obs, err := h.Proxy.Observe(r.Context())
	if err != nil {
		resp.DriftError = err.Error()
		return resp
	}
	plan, _, err := BuildProxyPlan(cfg, cfg, h.Proxy.Paths, obs)
	if err != nil {
		resp.DriftError = err.Error()
		return resp
	}
	view := viewProxyPlan(plan, false)
	resp.Drifted = !plan.Empty()
	resp.Drift = &view
	return resp
}
