package ingress

import (
	"bytes"
	"net/http"
	"strings"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// The module's REST surface (design.md §3.2 rule 2, §6.2).
//
// Deliberately thin: every decision here was made and tested in config.go,
// validate.go, plan.go and apply.go. If a rule appears in this file that is not
// in one of those, it is in the wrong place — the CLI would not get it.

// HTTP serves the module.
type HTTP struct {
	// Applier owns the read and write paths onto the system.
	Applier Applier

	// Lock is core's one global apply lock (§3.6). Writes take it; reads never
	// do.
	Lock *core.Lock

	// Events is where an applied change is announced so the UI can re-read.
	Events *core.Events
}

// Routes is the module's surface, declared as data so it can be enumerated
// rather than only served. Core mounts these with the /api/ingress prefix
// stripped.
func (h HTTP) Routes() []core.Route {
	return []core.Route{
		{
			Method: "GET", Path: "/config", Tool: "show config",
			Summary: "Show the stored ingress configuration: which services are published, and how certificates are obtained. " +
				"The provider credential is never returned.",
			Handler: h.getConfig,
		},
		{
			Method: "PUT", Path: "/config",
			Summary:  "Replace the whole ingress configuration.",
			Body:     core.BodyFull,
			Mutating: true,
			Handler:  h.putConfig,
		},
		{
			Method: "PATCH", Path: "/config",
			Summary:  "Change named ingress fields and leave the rest alone.",
			Body:     core.BodyRelaxed,
			Mutating: true,
			Handler:  h.patchConfig,
		},
		{
			Method: "POST", Path: "/plan", Tool: "show plan",
			Summary: "Show what an ingress change would do without doing it. " +
				"An empty body plans the stored configuration, which answers whether the box has drifted.",
			Body:    core.BodyRelaxed,
			Handler: h.postPlan,
		},
		{
			Method: "GET", Path: "/status", Tool: "status",
			Summary: "Show whether the proxy is running, when its certificate expires, and whether the box still matches the stored configuration.",
			Handler: h.getStatus,
		},
		{
			Method: "GET", Path: "/services", Tool: "show services",
			Summary: "List published services with their URLs and where each one currently points.",
			Handler: h.getServices,
		},
		{
			Method: "GET", Path: "/providers", Tool: "show providers",
			Summary: "List the DNS providers this build can obtain certificates through.",
			Handler: h.getProviders,
		},
	}
}

// Handler returns the module's routes.
func (h HTTP) Handler() http.Handler { return core.RouteTable(h.Routes()) }

// --- intent ---------------------------------------------------------------

func (h HTTP) getConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.Applier.Load()
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Redacted on the way out, always. This is the response a UI renders into a
	// form and the CLI prints to a terminal, and there is no caller that needs
	// the real value back — the only thing that reads it is the renderer, which
	// loads the config itself.
	core.WriteJSON(w, http.StatusOK, cfg.Redacted())
}

func (h HTTP) putConfig(w http.ResponseWriter, r *http.Request) {
	data, err := core.ReadBody(w, r)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	cfg, err := UnmarshalConfig(data)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.applyPreservingToken(w, r, cfg)
}

func (h HTTP) patchConfig(w http.ResponseWriter, r *http.Request) {
	current, err := h.Applier.Load()
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	currentJSON, err := MarshalConfig(current)
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	patch, err := core.ReadBody(w, r)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(bytes.TrimSpace(patch)) == 0 {
		core.WriteError(w, http.StatusBadRequest, "empty patch body")
		return
	}

	merged, err := core.MergePatch(currentJSON, patch)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	cfg, err := UnmarshalConfig(merged)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.applyPreservingToken(w, r, cfg)
}

// applyPreservingToken handles the round trip that redaction creates.
//
// GET /config returns the credential as `********`. A UI that renders that into
// a form and PUTs the form back would otherwise *store the mask* — the proxy
// would restart, authenticate with eight asterisks, and renewals would fail
// from then on. Nothing about that is visible at the moment it happens, which
// is the same shape of failure as everything else in this module worth guarding
// against.
//
// So the mask means "unchanged", which is the only thing it can honestly mean.
// The real value is reachable by sending a different one.
func (h HTTP) applyPreservingToken(w http.ResponseWriter, r *http.Request, cfg Config) {
	if cfg.Certificate.Token == RedactedToken {
		current, err := h.Applier.Load()
		if err != nil {
			core.WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		cfg.Certificate.Token = current.Certificate.Token
	}
	h.apply(w, r, cfg)
}

// applyResponse always carries the plan and the steps, successful or not
// (§5.3.2: there is no rollback, so a half-finished change stays half-finished
// and the honest thing is to say which steps landed).
type applyResponse struct {
	Plan  planView        `json:"plan"`
	Steps []Step          `json:"steps,omitempty"`
	Error *core.ErrorBody `json:"error,omitempty"`
}

func (h HTTP) apply(w http.ResponseWriter, r *http.Request, cfg Config) {
	// Validated before the lock is taken. Validation is pure (§5.3.1), so
	// holding the lock for it would only make a bad request slow down a good
	// one.
	if res := Validate(cfg, h.Applier.DNS, h.Applier.Devices); !res.OK() {
		core.WriteError(w, http.StatusUnprocessableEntity,
			"invalid ingress configuration", problems(res.Errors)...)
		return
	}

	var (
		result   ApplyResult
		applyErr error
	)
	if err := h.Lock.Do(r.Context(), func() error {
		result, applyErr = h.Applier.Apply(r.Context(), cfg)
		return nil
	}); err != nil {
		core.WriteError(w, http.StatusServiceUnavailable,
			"timed out waiting for the apply lock: "+err.Error())
		return
	}

	if len(result.Steps) > 0 {
		h.Events.Publish(core.Event{Type: core.EventApplied, Module: ModuleName})
	}

	resp := applyResponse{Plan: viewPlan(result.Plan), Steps: result.Steps}
	if applyErr != nil {
		resp.Error = &core.ErrorBody{Message: applyErr.Error()}
		core.WriteJSON(w, http.StatusInternalServerError, resp)
		return
	}
	core.WriteJSON(w, http.StatusOK, resp)
}

// --- dry run --------------------------------------------------------------

func (h HTTP) postPlan(w http.ResponseWriter, r *http.Request) {
	data, err := core.ReadBody(w, r)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	var cfg Config
	if len(bytes.TrimSpace(data)) == 0 {
		cfg, err = h.Applier.Load()
	} else {
		cfg, err = UnmarshalConfig(data)
		if err == nil && cfg.Certificate.Token == RedactedToken {
			// Same round trip as applyPreservingToken, and it matters here too:
			// planning with the mask would report a credential change that a
			// PUT of the same body would not make.
			var current Config
			if current, err = h.Applier.Load(); err == nil {
				cfg.Certificate.Token = current.Certificate.Token
			}
		}
	}
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	plan, err := h.plan(r, cfg)
	if err != nil {
		message := err.Error()
		if len(plan.Validation.Errors) > 0 {
			message = "invalid ingress configuration"
		}
		core.WriteError(w, http.StatusUnprocessableEntity, message,
			problems(plan.Validation.Errors)...)
		return
	}
	core.WriteJSON(w, http.StatusOK, viewPlan(plan))
}

// plan reads fresh system state and diffs cfg against it. No lock: §3.6 says
// reads never take one, and §5.4 requires reading reality rather than a cache
// for drift detection to mean anything.
func (h HTTP) plan(r *http.Request, cfg Config) (Plan, error) {
	obs, err := h.Applier.Observe(r.Context())
	if err != nil {
		return Plan{}, err
	}
	return BuildPlan(h.Applier.Backend, cfg, h.Applier.DNS, h.Applier.Devices, obs)
}

// --- observed -------------------------------------------------------------

type statusResponse struct {
	Enabled bool `json:"enabled"`

	// Domain is the suffix published names live under. Reported because it is
	// not in this module's config — it belongs to `dns` (§4.1) — and an
	// operator reading `ingress status` should not have to know that to find
	// out what their URLs look like.
	Domain string `json:"domain,omitempty"`

	// Service is what systemd knows. Absent, with ServiceError set, when the
	// query itself failed — normal on a developer box with no D-Bus, and it
	// must not take the rest of the answer down with it.
	Service      *ProxyStatus `json:"service,omitempty"`
	ServiceError string       `json:"service_error,omitempty"`

	// Certificate is the clock-dependent half of health (docs/ingress.md §8).
	// Reported beside the service state and never folded into it: a proxy that
	// is running perfectly while its renewals fail is exactly the situation
	// this module has to be able to describe.
	Certificate  CertState `json:"certificate"`
	CertError    string    `json:"certificate_error,omitempty"`
	Published    int       `json:"published"`
	Drifted      bool      `json:"drifted"`
	Drift        *planView `json:"drift,omitempty"`
	DriftError   string    `json:"drift_error,omitempty"`
	AsOf         time.Time `json:"as_of"`
	CertWarnings []string  `json:"certificate_warnings,omitempty"`
}

func (h HTTP) getStatus(w http.ResponseWriter, r *http.Request) {
	resp := statusResponse{AsOf: stamp()}

	cfg, err := h.Applier.Load()
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Enabled = cfg.Enabled
	resp.Published = len(cfg.Services)
	resp.Domain = strings.ToLower(strings.TrimSuffix(h.Applier.DNS.LocalDomain(), "."))

	// Each half of health is reported independently and neither can suppress
	// the other (§5.4).
	if svc, err := h.Applier.Proxy.Status(r.Context()); err != nil {
		resp.ServiceError = err.Error()
	} else {
		resp.Service = &svc
	}

	if certs, err := ReadCertificates(h.Applier.Paths.Data); err != nil {
		resp.CertError = err.Error()
	} else {
		resp.Certificate = CertificateState(certs, resp.Domain, resp.AsOf)
		resp.CertWarnings = certWarnings(cfg, resp.Certificate)
	}

	if plan, err := h.plan(r, cfg); err != nil {
		resp.DriftError = err.Error()
	} else {
		view := viewPlan(plan)
		resp.Drifted = !plan.Empty()
		resp.Drift = &view
	}

	core.WriteJSON(w, http.StatusOK, resp)
}

// certWarnings turns the certificate state into sentences.
//
// An active check, per docs/ingress.md §8: the whole failure mode here is that
// nothing looks wrong for weeks. A number in a JSON field satisfies nobody who
// is not already looking for it, so the condition is also stated in words that
// a status line can print without interpreting.
func certWarnings(cfg Config, state CertState) []string {
	if !cfg.Enabled {
		return nil
	}
	switch {
	case !state.Found:
		if len(cfg.Services) == 0 {
			return nil
		}
		return []string{
			"no certificate has been issued yet — this is normal for a few minutes after enabling, " +
				"and permanent if the DNS provider credential is wrong",
		}
	case state.ExpiresIn <= 0:
		return []string{"the certificate has expired, so every published name is refused by browsers now"}
	case state.RenewalOverdue:
		return []string{
			"renewal is overdue: the certificate should already have been replaced and was not, " +
				"so something has been failing quietly. It stops working entirely in " +
				core.Plural(state.ExpiresInDays, "day"),
		}
	}
	return nil
}

type servicesResponse struct {
	Services []serviceView `json:"services"`
	Domain   string        `json:"domain,omitempty"`
	AsOf     time.Time     `json:"as_of"`
}

func (h HTTP) getServices(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.Applier.Load()
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	domain := strings.ToLower(strings.TrimSuffix(h.Applier.DNS.LocalDomain(), "."))

	resp := servicesResponse{
		Services: make([]serviceView, 0, len(cfg.Services)),
		Domain:   domain,
		AsOf:     stamp(),
	}
	for _, s := range cfg.Services {
		resp.Services = append(resp.Services, viewService(s, domain, h.Applier.Devices))
	}
	core.WriteJSON(w, http.StatusOK, resp)
}

type providersResponse struct {
	Providers []string `json:"providers"`
}

func (h HTTP) getProviders(w http.ResponseWriter, _ *http.Request) {
	core.WriteJSON(w, http.StatusOK, providersResponse{Providers: Providers()})
}
