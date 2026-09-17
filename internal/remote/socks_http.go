package remote

import (
	"bytes"
	"encoding/json"
	"net/http"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// The SOCKS5 proxy's surface.
//
// The third of three under one module namespace, and it needed no new argument —
// which is what docs/remote-access.md §9 predicted when it deferred this object.
// Same route shape as Shadowsocks', same write path (mutateProxy, now given the
// applier to drive), same refusal semantics.
//
// One route differs in a way worth stating: `/socks5/link` returns a
// `socks5://` URL carrying the credential, so like its Shadowsocks counterpart
// it is **deliberately not published as an MCP tool**. The conformance suite's
// R2 wants every read to be a tool and wants exceptions argued rather than
// assumed; the argument is that the response *is* the secret, and a tool whose
// purpose is to return one would put it in a transcript the first time somebody
// asked a model how remote access is set up.

func (h HTTP) socksRoutes() []core.Route {
	return []core.Route{
		{
			Method: "GET", Path: "/socks5/config", Tool: "show socks5",
			Summary: "Show the stored SOCKS5 configuration: the port, where it listens and the " +
				"account name. The password is never returned here.",
			Handler: h.getSocksConfig,
		},
		{
			Method: "PATCH", Path: "/socks5/config",
			Summary: "Change the SOCKS5 proxy's settings, or turn it on and off. " +
				"Moving it between the tunnel and the internet changes who can reach it, " +
				"so every device has to be given the new link.",
			Body:     core.BodyRelaxed,
			Query:    gate(),
			Mutating: true,
			Handler:  h.patchSocksConfig,
		},
		{
			// No Tool, for the reason ss_http.go argues about its own link
			// route: the response is the credential.
			Method: "GET", Path: "/socks5/link",
			Summary: "Return the socks5:// link a client imports. This contains the password, " +
				"so unlike every other read here it is not redacted.",
			Handler: h.getSocksLink,
		},
		{
			Method: "POST", Path: "/socks5/plan", Tool: "show socks5 plan",
			Summary: "Show what a SOCKS5 change would do without doing it. " +
				"An empty body plans the stored configuration, which answers whether the box has drifted.",
			Body:    core.BodyRelaxed,
			Handler: h.postSocksPlan,
		},
		{
			Method: "POST", Path: "/socks5/apply",
			Summary: "Re-render the SOCKS5 configuration and restart it from stored intent, " +
				"changing no intent. This is the repair path for a half-applied change or a " +
				"file somebody edited.",
			Mutating: true,
			Handler:  h.postSocksApply,
		},
		{
			Method: "GET", Path: "/socks5/status", Tool: "status socks5",
			Summary: "Show whether the SOCKS5 proxy is installed and running, where it listens, " +
				"and whether the box still matches the stored configuration.",
			Handler: h.getSocksStatus,
		},
	}
}

// --- intent -----------------------------------------------------------------

func (h HTTP) getSocksConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.Socks.Load()
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	core.WriteJSON(w, http.StatusOK, cfg.Redacted().Socks)
}

func (h HTTP) patchSocksConfig(w http.ResponseWriter, r *http.Request) {
	patch, err := core.ReadBody(w, r)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(bytes.TrimSpace(patch)) == 0 {
		core.WriteError(w, http.StatusBadRequest, "empty patch body")
		return
	}

	h.mutateProxy(w, r, false, h.Socks, func(cfg *Config) error {
		next, err := mergeSocks(cfg.Socks, patch)
		if err != nil {
			return err
		}
		cfg.Socks = next
		return nil
	})
}

// mergeSocks applies an RFC 7386 merge patch to the proxy's section.
//
// The mask is treated as "unchanged", which is the only thing it can honestly
// mean: a UI that rendered the redacted value into a form and sent it back would
// otherwise store eight asterisks as the password and take every client offline.
func mergeSocks(current Socks5, patch []byte) (Socks5, error) {
	encoded, err := json.Marshal(current)
	if err != nil {
		return Socks5{}, err
	}
	merged, err := core.MergePatch(encoded, patch)
	if err != nil {
		return Socks5{}, badRequest(err)
	}
	next, err := UnmarshalSocks(merged)
	if err != nil {
		return Socks5{}, badRequest(err)
	}
	if next.Password == RedactedSecret {
		next.Password = current.Password
	}
	return next, nil
}

func (h HTTP) postSocksApply(w http.ResponseWriter, r *http.Request) {
	// Exempt from the confirm gate: this re-programs intent that was already
	// confirmed when it was stored, so there is no new decision to put to
	// anybody — and a box whose file somebody deleted is the one that most
	// needs repairing.
	h.mutateProxy(w, r, true, h.Socks, func(*Config) error { return nil })
}

// --- dry run ----------------------------------------------------------------

func (h HTTP) postSocksPlan(w http.ResponseWriter, r *http.Request) {
	data, err := core.ReadBody(w, r)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	cfg, err := h.Socks.Load()
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	previous := cfg.Clone()

	if len(bytes.TrimSpace(data)) > 0 {
		merged, err := mergeSocks(cfg.Socks, data)
		if err != nil {
			core.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		cfg.Socks = merged
	}
	if cfg.Socks.Enabled {
		next, _, err := cfg.Socks.WithGeneratedPassword()
		if err != nil {
			core.WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		cfg.Socks = next
	}
	cfg.Normalize()

	obs, err := h.Socks.Observe(r.Context())
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	plan, _, err := BuildSocksPlan(cfg, previous, h.Socks.Paths, obs)
	if err != nil {
		core.WriteError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	core.WriteJSON(w, http.StatusOK, viewProxyPlan(plan, false))
}

// --- the credential ----------------------------------------------------------

func (h HTTP) getSocksLink(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.Socks.Load()
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	url, err := SocksClientURL(cfg)
	if err != nil {
		// 409 rather than 500: nothing is broken, the configuration is simply
		// not far enough along to produce a link, and the message says which
		// half is missing.
		core.WriteError(w, http.StatusConflict, err.Error())
		return
	}
	core.WriteJSON(w, http.StatusOK, linkResponse{
		URL: url, Label: cfg.EndpointHost(), AsOf: stamp(),
	})
}

// --- observed ---------------------------------------------------------------

// socksStatus is the SOCKS5 half of health.
//
// Not proxyStatus: that one carries a cipher and a UDP flag, and this object has
// neither. What it has instead is the field that decides everything about it —
// where it listens.
type socksStatus struct {
	Enabled bool        `json:"enabled"`
	Port    uint16      `json:"listen_port"`
	Listen  ListenScope `json:"listen"`

	// Exposed is `listen` reduced to the question a surface actually asks, so
	// that a UI does not have to know which scope values mean "on the internet".
	Exposed bool `json:"exposed"`

	// Warning is SocksExposureWarning when the proxy is exposed, and empty
	// otherwise. Carried here rather than left to each client to reproduce:
	// there is one wording, and a second copy would drift from it.
	Warning string `json:"warning,omitempty"`

	Service      *core.UnitStatus `json:"service,omitempty"`
	ServiceError string           `json:"service_error,omitempty"`

	Binary      string `json:"binary,omitempty"`
	BinaryError string `json:"binary_error,omitempty"`

	Drifted    bool           `json:"drifted"`
	Drift      *proxyPlanView `json:"drift,omitempty"`
	DriftError string         `json:"drift_error,omitempty"`
	AsOf       time.Time      `json:"as_of"`
}

func (h HTTP) getSocksStatus(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.Socks.Load()
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s := cfg.Socks
	out := socksStatus{
		Enabled: s.Enabled,
		Port:    s.PortOrDefault(),
		Listen:  s.ListenScopeOrDefault(),
		Exposed: s.ListenScopeOrDefault().Exposed(),
		AsOf:    stamp(),
	}
	if out.Exposed && s.Enabled {
		out.Warning = SocksExposureWarning
	}

	if status, err := h.Socks.Unit.Status(r.Context()); err != nil {
		out.ServiceError = err.Error()
	} else {
		out.Service = &status
	}

	if path, err := FindSocks(); err != nil {
		out.BinaryError = ErrSocksMissing().Error()
	} else {
		out.Binary = path
	}

	// Drift is asked of the stored configuration, so it answers "does the box
	// still match what olr was told" and not "would this edit change anything".
	if plan, _, err := h.Socks.Plan(r.Context(), cfg); err != nil {
		out.DriftError = err.Error()
	} else if !plan.Empty() {
		view := viewProxyPlan(plan, false)
		out.Drifted, out.Drift = true, &view
	}

	core.WriteJSON(w, http.StatusOK, out)
}
