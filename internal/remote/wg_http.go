package remote

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// The module's REST surface (design.md §3.2 rule 2, §6.2).
//
// Deliberately thin: every decision here was made and tested in config.go,
// validate.go, plan.go and apply.go. If a rule appears in this file that is not
// in one of those, it is in the wrong place — the CLI would not get it.
//
// One thing genuinely does live here and nowhere else, and it is the exception
// that proves the rule: **creating a peer is the only operation in olr whose
// response cannot be reproduced.** The client configuration is generated,
// returned, and forgotten, so the code that hands it back has to sit inside the
// same locked edit that stored the peer.

// wireguardRoutes is the tunnel's half of the module's surface.
//
// Under its own prefix rather than at the module root, which is the change the
// second object forced. The two objects plan and apply completely differently,
// so a shared `/config` would have to own a plan covering both — and a caller
// editing a cipher would be told about a kernel interface.
func (h HTTP) wireguardRoutes() []core.Route {
	return []core.Route{
		{
			Method: "GET", Path: "/wireguard/config", Tool: "show wireguard",
			Summary: "Show the stored tunnel configuration: the dial-in network, its settings and the devices that may connect. " +
				"The box's private key is never returned.",
			Handler: h.getWireGuardConfig,
		},
		{
			Method: "PUT", Path: "/wireguard/config",
			Summary:  "Replace the whole tunnel configuration.",
			Body:     core.BodyFull,
			Query:    gate(),
			Mutating: true,
			Handler:  h.putWireGuardConfig,
		},
		{
			Method: "PATCH", Path: "/wireguard/config",
			Summary: "Change the tunnel's settings, or turn it on and off, leaving the devices alone. " +
				"Cannot edit devices; use the item routes for those.",
			Body:     core.BodyRelaxed,
			Query:    gate(),
			Mutating: true,
			Handler:  h.patchWireGuardConfig,
		},

		// One route per device, because RFC 7386 replaces an array wholesale
		// rather than merging into it — and here "replaces wholesale" would
		// mean revoking everybody else.
		{
			Method: "PUT", Path: "/wireguard/peers/{name}",
			Summary: "Add a device that may dial in, or change one, leaving every other device alone. " +
				"A new device's client configuration is returned once and cannot be shown again.",
			Query:    gate(),
			Mutating: true,
			Handler:  h.putPeer,
		},
		{
			Method: "DELETE", Path: "/wireguard/peers/{name}",
			Summary:  "Stop a device dialling in. Its access ends immediately, so this is refused without confirm.",
			Query:    gate(),
			Mutating: true,
			Handler:  h.deletePeer,
		},

		{
			Method: "POST", Path: "/wireguard/plan", Tool: "show wireguard plan",
			Summary: "Show what a tunnel change would do without doing it. " +
				"An empty body plans the stored configuration, which answers whether the box has drifted.",
			Body:    core.BodyRelaxed,
			Handler: h.postPlan,
		},
		{
			Method: "POST", Path: "/wireguard/apply",
			Summary: "Re-program the tunnel from the stored configuration, changing no intent. " +
				"This is the repair path after a reboot, or for an interface somebody removed.",
			Mutating: true,
			Handler:  h.postApply,
		},

		{
			Method: "GET", Path: "/wireguard/status", Tool: "status wireguard",
			Summary: "Show whether the tunnel exists and is up, when each device last connected, " +
				"and whether the box still matches the stored configuration.",
			Handler: h.getWireGuardStatus,
		},
		{
			Method: "GET", Path: "/wireguard/peers", Tool: "show peers",
			Summary: "List the devices that may dial in, with the address each holds and when it last connected.",
			Handler: h.getPeers,
		},
	}
}

// --- intent -----------------------------------------------------------------

func (h HTTP) getWireGuardConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.Tunnel.Load()
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Redacted on the way out, always. This is the response a UI renders into a
	// form and the CLI prints to a terminal, and no caller needs the real value
	// back — the only thing that reads it is the renderer, which loads the
	// config itself.
	core.WriteJSON(w, http.StatusOK, cfg.Redacted())
}

func (h HTTP) putWireGuardConfig(w http.ResponseWriter, r *http.Request) {
	data, err := core.ReadBody(w, r)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	// The tunnel's section, not the whole document. A route under
	// /wireguard/config that accepted the document could change the proxy's
	// cipher through a plan that never looks at the proxy.
	next, err := UnmarshalWireGuard(data)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.mutate(w, r, func(cfg *Config) (*peerResult, error) {
		cfg.WireGuard = preserveKey(next, cfg.WireGuard)
		return nil, nil
	})
}

func (h HTTP) patchWireGuardConfig(w http.ResponseWriter, r *http.Request) {
	patch, err := core.ReadBody(w, r)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(bytes.TrimSpace(patch)) == 0 {
		core.WriteError(w, http.StatusBadRequest, "empty patch body")
		return
	}

	h.mutate(w, r, func(cfg *Config) (*peerResult, error) {
		current, err := json.Marshal(cfg.WireGuard)
		if err != nil {
			return nil, err
		}
		merged, err := core.MergePatch(current, patch)
		if err != nil {
			return nil, badRequest(err)
		}
		// Strict again on the merged result, so an unknown key in the patch is
		// still caught rather than smuggled in by the merge.
		next, err := UnmarshalWireGuard(merged)
		if err != nil {
			return nil, badRequest(err)
		}
		cfg.WireGuard = preserveKey(next, cfg.WireGuard)
		return nil, nil
	})
}

// peerBody is what a client sends to create or change a peer.
//
// Not Peer itself, and the difference is the address: a peer's address is
// allocated by the daemon and fixed for its lifetime, so accepting one from a
// client would let a caller renumber a device whose configuration is already on
// a phone. It is absent from this struct rather than ignored, so that sending
// one is an error rather than a silent no-op.
type peerBody struct {
	// Routes is what the device sends through the tunnel. Empty means
	// RouteHome.
	Routes RouteScope `json:"routes,omitempty"`

	// PublicKey is set by an operator who generated the key pair on the device
	// themselves. Empty means olr generates one, which is the ordinary path and
	// the one that produces a complete client configuration.
	PublicKey string `json:"public_key,omitempty"`
}

func (h HTTP) putPeer(w http.ResponseWriter, r *http.Request) {
	name := normalizeName(r.PathValue("name"))

	var body peerBody
	if r.ContentLength != 0 {
		if err := core.DecodeJSON(w, r, &body); err != nil {
			core.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	h.mutate(w, r, func(cfg *Config) (*peerResult, error) {
		if existing, ok := cfg.WireGuard.Peer(name); ok {
			// An edit. No new key and no new address — the device's file names
			// both, and olr cannot reach it to say otherwise.
			was := existing.Routes.OrDefault()
			existing.Routes = body.Routes
			if body.PublicKey != "" {
				existing.PublicKey = body.PublicKey
			}
			cfg.WireGuard.SetPeer(existing)

			if existing.Routes.OrDefault() == was {
				return nil, nil
			}
			// **The change has to be carried by hand, and saying nothing here
			// would be a lie.** What a device sends into the tunnel is decided
			// entirely by its own `AllowedIPs`; the tunnel does not know and
			// cannot be told. So this edit changes the stored intent, produces
			// no kernel change at all, and takes effect only when somebody
			// edits one line on the device — which is the one thing the
			// response can usefully carry.
			return &peerResult{
				Name:    name,
				Address: existing.Address,
				Note: fmt.Sprintf(
					"What %s sends through the tunnel is decided by the file on the device, and olr "+
						"cannot rewrite it — the private key in it was never stored. Change one line "+
						"there:\n\n    AllowedIPs = %s",
					name, describePrefixes(ClientRoutes(cfg.WireGuard, existing, networksOf(h.Tunnel.Networks)))),
			}, nil
		}

		addr, ok := cfg.WireGuard.NextAddress()
		if !ok {
			return nil, fmt.Errorf(
				"%s has no free addresses left; widen the dial-in network with `olr remote set --subnet <prefix>`",
				cfg.WireGuard.SubnetOrDefault())
		}

		peer := Peer{Name: name, Address: addr, Routes: body.Routes, PublicKey: body.PublicKey}
		created := &peerResult{Name: name, Address: addr}

		private := ""
		if body.PublicKey == "" {
			pair, err := GenerateKey()
			if err != nil {
				return nil, err
			}
			peer.PublicKey, private = pair.Public, pair.Private
		} else {
			created.Note = "You supplied the public key, so olr has no private key to write. " +
				"Fill the PrivateKey line in on the device."
		}
		cfg.WireGuard.SetPeer(peer)

		// Rendered from the config as it now stands, inside the lock, because
		// this is the only moment the private key exists anywhere.
		conf, err := ClientConfig(*cfg, peer, private, networksOf(h.Tunnel.Networks))
		if err != nil {
			return nil, err
		}
		created.ClientConfig = conf
		if private != "" {
			created.Note = "This is the only time olr can show this file: the private key in it is " +
				"not stored. Import it on the device now."
		}
		// A local name belongs to `dns` and is not ours to write (see
		// peerResult.NextSteps). Offered here because this is the one moment
		// the operator has the address in front of them.
		created.NextSteps = append(created.NextSteps, fmt.Sprintf(
			"olr dns add host %s --address %s", name, addr))
		return created, nil
	})
}

func (h HTTP) deletePeer(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	h.mutate(w, r, func(cfg *Config) (*peerResult, error) {
		if !cfg.WireGuard.RemovePeer(name) {
			// 404, not 422. "There is no device by that name" is a statement
			// about the address, and a UI retrying a delete it already
			// completed should be able to tell that from a refusal.
			return nil, notFound(fmt.Errorf("no device named %q may dial in", name))
		}
		return nil, nil
	})
}

// preserveKey closes the round trip redaction creates.
//
// GET /config returns the private key as `********`. A UI that renders that
// into a form and sends the form back would otherwise *store the mask* — the
// tunnel would come up with a key that is eight asterisks, every client
// configuration ever issued would name a public key the box no longer has, and
// nothing about that is visible at the moment it happens.
//
// So the mask means "unchanged", which is the only thing it can honestly mean.
func preserveKey(next, current WireGuard) WireGuard {
	if next.PrivateKey == RedactedSecret {
		next.PrivateKey = current.PrivateKey
	}
	return next
}

// applyResponse always carries the plan and the steps, successful or not
// (§5.3.2: there is no rollback, so a half-finished change stays half-finished
// and the honest thing is to say which steps landed).
type tunnelApplyResponse struct {
	Plan  planView        `json:"plan"`
	Steps []Step          `json:"steps,omitempty"`
	Error *core.ErrorBody `json:"error,omitempty"`

	// Config is what is stored now, redacted. Returned on the refusal path
	// especially: a client that has just been told "no" needs to know the
	// document did not move.
	Config *Config `json:"config,omitempty"`

	// Peer is the one-shot client configuration, present only on the response
	// to the request that created a peer.
	Peer *peerResult `json:"peer,omitempty"`
}

// mutate is the one write path every mutating route goes through.
//
// Load, edit, validate, observe, plan, and only then write — all inside the one
// global apply lock (§3.6). The lock has to cover both halves or it covers
// nothing that matters: two clients adding a device at once would otherwise
// each allocate the same address.
func (h HTTP) mutate(w http.ResponseWriter, r *http.Request, edit func(*Config) (*peerResult, error)) {
	h.mutateWith(w, r, false, edit)
}

// mutateWith is mutate with the disruptive gate optionally already satisfied.
//
// forceConfirm is set by POST /apply alone. That route re-applies intent the
// operator stored earlier — and confirmed then, if it needed confirming — so
// there is no new decision to put to them. Gating it would mean a box whose
// tunnel somebody deleted needs an extra flag to be repaired, and the broken
// box is the one that most needs repairing.
func (h HTTP) mutateWith(w http.ResponseWriter, r *http.Request, forceConfirm bool, edit func(*Config) (*peerResult, error)) {
	dryRun, err := boolParam(r, "dry_run")
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	confirm, err := boolParam(r, "confirm")
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	confirm = confirm || forceConfirm

	var (
		editErr  error
		invalid  Result
		failed   error
		plan     Plan
		desired  Desired
		before   []string
		result   ApplyResult
		stored   Config
		applyErr error
		created  *peerResult
		held     bool
	)

	if lockErr := h.Lock.Do(r.Context(), func() error {
		cfg, err := h.Tunnel.Load()
		if err != nil {
			failed = err
			return nil
		}
		// Kept before the edit mutates it, so the plan can still name a device
		// the edit removes (BuildPlan). A deep clone, not a copy: the edit
		// splices Peers in place and a shallow copy shares that array — which
		// is also what makes it usable below as "what is still stored".
		previous := cfg.Clone()
		if created, err = edit(&cfg); err != nil {
			editErr = err
			return nil
		}

		// The key is filled in rather than demanded. It is derived state, not a
		// choice — nothing an operator could type would be better than random
		// bytes — so this is the same move as deriving a DHCP range from a
		// prefix, not the inferred behaviour design.md §5.6 forbids.
		if cfg.WireGuard.Enabled || len(cfg.WireGuard.Peers) > 0 {
			if cfg, err = GenerateFor(cfg); err != nil {
				failed = err
				return nil
			}
		}
		cfg.Normalize()

		res := ValidateWireGuard(cfg, h.Tunnel.Networks)
		validateEndpoint(&res, cfg)
		if !res.OK() {
			invalid = res
			return nil
		}

		obs, err := h.Tunnel.ObserveFor(r.Context(), cfg)
		if err != nil {
			failed = err
			return nil
		}
		before = obs.Lines
		plan, desired, err = BuildPlan(cfg, previous, h.Tunnel.Networks, obs)
		if err != nil {
			failed = err
			return nil
		}

		// Three reasons to stop with the plan and write nothing. A dry run
		// asked the question without wanting it acted on; an unconfirmed
		// disruptive change is §5.3.3's one interruption; and a blocked plan
		// cannot proceed at all, which is not a decision the operator can make
		// from here.
		if dryRun || plan.Blocked != "" || (plan.Impact == ImpactDisruptive && !confirm) {
			held = true
			// Nothing was stored, so nothing was generated either — and a
			// client configuration for a peer that does not exist would be the
			// worst possible thing to hand somebody.
			created = nil
			// The clone taken before the edit, which is what is still on disk.
			// Using `cfg` here would return the *proposed* document to a client
			// that has just been told it was not applied.
			stored = previous
			return nil
		}

		result, applyErr = h.Tunnel.ApplyPlanned(r.Context(), cfg, plan, desired)
		stored, _ = h.Tunnel.Load()
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

	view := viewPlan(plan, before, desired.Lines())

	// A dry run answers with the plan alone, the same shape POST /plan gives, so
	// a caller that asked "what would this do?" gets one answer regardless of
	// which route it asked down.
	if dryRun {
		core.WriteJSON(w, http.StatusOK, view)
		return
	}

	redacted := stored.Redacted()

	if held {
		core.WriteJSON(w, http.StatusConflict, tunnelApplyResponse{
			Plan:   view,
			Config: &redacted,
			Error:  &core.ErrorBody{Message: tunnelRefusal(plan)},
		})
		return
	}

	// Published whenever anything was attempted, including a partial failure —
	// especially then. Something on the box changed and every client's idea of
	// it is now stale.
	if len(result.Steps) > 0 {
		h.Events.Publish(core.Event{Type: core.EventApplied, Module: ModuleName})
	}

	resp := tunnelApplyResponse{Plan: view, Steps: result.Steps, Config: &redacted, Peer: created}
	if applyErr != nil {
		resp.Error = &core.ErrorBody{Message: applyErr.Error()}
		core.WriteJSON(w, http.StatusInternalServerError, resp)
		return
	}
	core.WriteJSON(w, http.StatusOK, resp)
}

// refusal says why nothing was written, in the operator's terms.
//
// Two shapes of 409 share this body, the way internal/gateway's do: the
// interface name being taken is somebody else's problem to resolve, while a
// disruptive plan is a decision to make here. The plan already worked both out,
// so repeating its words rather than inventing a sentence keeps the refusal and
// the preview saying the same thing.
func tunnelRefusal(plan Plan) string {
	if plan.Blocked != "" {
		return plan.Blocked
	}
	if len(plan.Reasons) > 0 {
		return strings.Join(plan.Reasons, "; ") + "; repeat the request with confirm=true to go ahead"
	}
	return "this would take somebody's access away; repeat the request with confirm=true to go ahead"
}

// --- re-apply ---------------------------------------------------------------

func (h HTTP) postApply(w http.ResponseWriter, r *http.Request) {
	h.mutateWith(w, r, true, func(*Config) (*peerResult, error) { return nil, nil })
}

// --- dry run ----------------------------------------------------------------

func (h HTTP) postPlan(w http.ResponseWriter, r *http.Request) {
	data, err := core.ReadBody(w, r)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	stored, err := h.Tunnel.Load()
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	cfg := stored
	if len(bytes.TrimSpace(data)) > 0 {
		if cfg, err = UnmarshalConfig(data); err != nil {
			core.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		// Same round trip as preserveKey, and it matters here too: planning
		// with the mask would report a key change that a PUT of the same body
		// would not make.
		cfg.WireGuard = preserveKey(cfg.WireGuard, stored.WireGuard)
	}

	obs, err := h.Tunnel.ObserveFor(r.Context(), cfg)
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	plan, desired, err := BuildPlan(cfg, stored, h.Tunnel.Networks, obs)
	if err != nil {
		message := err.Error()
		if len(plan.Validation.Errors) > 0 {
			message = "invalid remote-access configuration"
		}
		core.WriteError(w, http.StatusUnprocessableEntity, message,
			problems(plan.Validation.Errors)...)
		return
	}
	core.WriteJSON(w, http.StatusOK, viewPlan(plan, obs.Lines, desired.Lines()))
}

// --- observed ---------------------------------------------------------------

type tunnelStatus struct {
	Enabled   bool         `json:"enabled"`
	Interface string       `json:"interface"`
	Port      uint16       `json:"listen_port"`
	Endpoint  string       `json:"endpoint,omitempty"`
	Subnet    netip.Prefix `json:"subnet,omitempty"`
	Address   netip.Addr   `json:"address,omitempty"`

	// PublicKey is this box's, and is not a secret: it is in every client
	// configuration already. Reported because it is the one value an operator
	// needs when completing a configuration by hand.
	PublicKey string `json:"public_key,omitempty"`

	// Present, Up and Foreign are what the kernel holds. Known is false where
	// it could not be read at all, which must not be reported as "the tunnel is
	// down" (design.md §3.4).
	Known   bool `json:"kernel_known"`
	Present bool `json:"interface_present"`
	Up      bool `json:"interface_up"`
	Foreign bool `json:"interface_foreign,omitempty"`

	// Blockers are the things standing in this module's way — a missing
	// `wireguard-tools`, above all. Reported even while remote access is off,
	// because that is when an operator can act on them without a failure first.
	Blockers []core.Blocker `json:"blockers,omitempty"`

	Peers []peerView `json:"peers"`

	Drifted    bool      `json:"drifted"`
	Drift      *planView `json:"drift,omitempty"`
	DriftError string    `json:"drift_error,omitempty"`
	AsOf       time.Time `json:"as_of"`
}

func (h HTTP) getWireGuardStatus(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.Tunnel.Load()
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	core.WriteJSON(w, http.StatusOK, h.tunnelStatusFor(r, cfg))
}

// tunnelStatusFor is the tunnel's half of health, so that the module-level
// status can report it beside the proxy's without a second implementation.
func (h HTTP) tunnelStatusFor(r *http.Request, cfg Config) tunnelStatus {
	resp := tunnelStatus{
		AsOf:      stamp(),
		Enabled:   cfg.WireGuard.Enabled,
		Interface: cfg.WireGuard.InterfaceOrDefault(),
		Port:      cfg.WireGuard.PortOrDefault(),
		Endpoint:  cfg.Endpoint,
		Subnet:    cfg.WireGuard.SubnetOrDefault(),
		Address:   cfg.WireGuard.RouterAddr(),
		Blockers:  Blockers(),
	}
	if public, err := PublicKeyFor(cfg.WireGuard.PrivateKey); err == nil {
		resp.PublicKey = public
	}

	obs, err := h.Tunnel.ObserveFor(r.Context(), cfg)
	if err != nil {
		// The observation failed, which is different from the tunnel being
		// down. Both halves of health are reported independently and neither
		// can suppress the other (§5.4), so the config half above still stands.
		resp.DriftError = err.Error()
		resp.Peers = viewPeers(cfg, Observed{}, resp.AsOf)
		return resp
	}

	resp.Known, resp.Present, resp.Up, resp.Foreign = obs.Known, obs.Present, obs.Up, obs.Foreign
	resp.Peers = viewPeers(cfg, obs, resp.AsOf)

	plan, desired, err := BuildPlan(cfg, cfg, h.Tunnel.Networks, obs)
	if err != nil {
		resp.DriftError = err.Error()
		return resp
	}
	view := viewPlan(plan, obs.Lines, desired.Lines())
	resp.Drifted = !plan.Empty()
	resp.Drift = &view
	return resp
}

type peersResponse struct {
	Peers []peerView `json:"peers"`

	// Subnet is reported beside the list because a peer's address is
	// meaningless without it, and it is not on any peer.
	Subnet netip.Prefix `json:"subnet,omitempty"`
	AsOf   time.Time    `json:"as_of"`
}

func (h HTTP) getPeers(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.Tunnel.Load()
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// A failed observation is not a failed list: the stored half is still the
	// answer to "who may dial in", and peerView.Unknown says the live half is
	// missing rather than empty.
	obs, _ := h.Tunnel.ObserveFor(r.Context(), cfg)

	resp := peersResponse{
		Peers:  viewPeers(cfg, obs, stamp()),
		Subnet: cfg.WireGuard.SubnetOrDefault(),
		AsOf:   stamp(),
	}
	core.WriteJSON(w, http.StatusOK, resp)
}
