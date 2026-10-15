package system

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// HTTP is this module's REST surface.
type HTTP struct {
	Applier          Applier
	Proc             string
	Lock             *core.Lock
	Events           *core.Events
	Sessions         *Sessions
	TokenRequired    bool
	ValidToken       func(*http.Request) bool
	PublicIPv4Client *http.Client
	PublicIPv6Client *http.Client
}

// Routes is the module's surface, declared as data so it can be enumerated
// rather than only served.
func (h HTTP) Routes() []core.Route {
	return []core.Route{
		{Method: "GET", Path: "/metrics", Tool: "show metrics", Summary: "Host uptime, CPU and memory utilization.", Handler: h.getMetrics},
		{Method: "GET", Path: "/public-addresses", Tool: "show public addresses", Summary: "IPv4 and IPv6 addresses seen by ip.sb from this router.", Handler: h.getPublicAddresses},
		{
			Method: "GET", Path: "/access", Tool: "show access",
			Summary: "Show whether this router has been set up, and whether reaching it over " +
				"the network needs a password.",
			Handler: h.getAccess,
		},
		{
			Method: "POST", Path: "/access/claim",
			Summary: "Set this router up for the first time, recording whether reaching it " +
				"over the network needs a password. Works only once, and only from the " +
				"local network or the box itself.",
			Body:     core.BodyFull,
			Mutating: true,
			Handler:  h.postClaim,
		},
		{Method: "POST", Path: "/access/login", Summary: "Sign in with the administrator password; creates a temporary browser session.", Mutating: true, Handler: h.postLogin},
		{Method: "POST", Path: "/access/logout", Summary: "End this browser's administrator session.", Mutating: true, Handler: h.postLogout},
		{Method: "PUT", Path: "/access/password", Summary: "Set or remove the administrator password; revokes previous browser sessions. The Unix socket is the local recovery path.", Mutating: true, Handler: h.putPassword},
	}
}

// Handler returns the module's routes.
func (h HTTP) Handler() http.Handler { return core.RouteTable(h.Routes()) }

// AccessView is what GET /access answers.
//
// Deliberately says nothing a caller could use to guess the password — not its
// length, not its age, not its hash. The two booleans are what a UI needs to
// decide which screen to draw, and nothing else here is anybody's business.
type AccessView struct {
	// Claimed is false on a box nobody has set up, which is what makes the SPA
	// draw the onboarding screen rather than the dashboard.
	Claimed bool `json:"claimed"`

	// PasswordSet reports whether reaching this box over the network needs one.
	PasswordSet   bool `json:"password_set"`
	Authenticated bool `json:"authenticated"`
	TokenRequired bool `json:"token_required"`
}

func (h HTTP) getAccess(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.Applier.Load()
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	authenticated := !cfg.RequiresPassword() && !h.TokenRequired
	if cfg.RequiresPassword() {
		authenticated = h.Sessions.Valid(r, cfg.Access.Password)
	}
	if h.ValidToken != nil && h.ValidToken(r) {
		authenticated = true
	}
	w.Header().Set("Cache-Control", "no-store")
	core.WriteJSON(w, http.StatusOK, AccessView{
		Claimed:       cfg.Claimed(),
		PasswordSet:   cfg.RequiresPassword(),
		Authenticated: authenticated,
		TokenRequired: h.TokenRequired && !cfg.RequiresPassword(),
	})
}

// ClaimRequest is the body of POST /access/claim.
type ClaimRequest struct {
	// Password is the credential to require, or empty for none.
	//
	// Empty string means "no password", and it has to be sent deliberately —
	// see the comment on NoPassword. There is no omitempty: the field's absence
	// and an empty value must not be the same request.
	Password string `json:"password" jsonschema:"description=Password to require over the network. Empty with no_password set means none."`

	// NoPassword must be true when Password is empty.
	//
	// A second field to express "none" looks redundant and is not. Without it,
	// a UI bug that sent an empty string — an unfilled form, a lost variable —
	// would silently claim the box with no password, and claiming happens once.
	// Requiring the caller to say "yes, none, on purpose" makes the dangerous
	// outcome impossible to reach by accident, which is the same reason an
	// absent access section and a null password are different states.
	NoPassword bool `json:"no_password" jsonschema:"description=Must be true to claim with no password at all."`
}

func (h HTTP) postClaim(w http.ResponseWriter, r *http.Request) {
	var req ClaimRequest
	if err := core.DecodeJSON(w, r, &req); err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Who may claim: docs/system.md §4. The box itself and the local network,
	// never the public internet — the case this exists for is olr installed on a
	// VPS, where the first thing to find an open port is a scanner rather than
	// its owner. Checked here rather than in the gate because it is a property
	// of *this route* and not of the API.
	if !localSource(r) {
		core.WriteError(w, http.StatusForbidden,
			"this router can only be set up from its own network. Reach it from a machine "+
				"on the LAN, or run `sudo olr claim` on the box over SSH")
		return
	}

	password, err := h.passwordFor(r, req)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Claim only once, with the chosen derived credential or deliberate none.
	var cfg Config
	var claimErr error
	if err := h.Lock.Do(r.Context(), func() error {
		cfg, claimErr = h.Applier.Claim(password)
		return nil
	}); err != nil {
		core.WriteError(w, http.StatusServiceUnavailable, "waiting for the apply lock: "+err.Error())
		return
	}
	switch {
	case errors.Is(claimErr, ErrAlreadyClaimed):
		core.WriteError(w, http.StatusConflict, claimErr.Error())
		return
	case claimErr != nil:
		core.WriteError(w, http.StatusInternalServerError, claimErr.Error())
		return
	}

	core.WriteJSON(w, http.StatusOK, AccessView{
		Claimed:     cfg.Claimed(),
		PasswordSet: cfg.RequiresPassword(),
	})
}

func (h HTTP) passwordFor(r *http.Request, req ClaimRequest) (*Password, error) {
	if req.Password == "" && req.NoPassword {
		return nil, nil
	}
	if req.Password == "" || req.NoPassword {
		return nil, errors.New("provide a password, or explicitly choose no_password")
	}
	if h.Sessions != nil {
		finish, ok := h.Sessions.Begin(r)
		if !ok {
			return nil, errors.New("too many password requests; try again in a minute")
		}
		defer finish()
	}
	return HashPassword(req.Password)
}

func (h HTTP) postLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
	}
	if err := core.DecodeJSON(w, r, &req); err != nil {
		core.WriteError(w, 400, err.Error())
		return
	}
	if h.Sessions == nil {
		core.WriteError(w, 503, "password sessions are unavailable")
		return
	}
	finish, allowed := h.Sessions.Begin(r)
	if !allowed {
		w.Header().Set("Retry-After", "60")
		core.WriteError(w, 429, "too many sign-in attempts; try again in a minute")
		return
	}
	defer finish()
	cfg, err := h.Applier.Load()
	if err != nil {
		core.WriteError(w, 503, "cannot read administrator access configuration")
		return
	}
	if !cfg.RequiresPassword() || !cfg.Access.Password.Matches(req.Password) {
		core.WriteError(w, 401, "invalid administrator password")
		return
	}
	// Check again under the write lock: a password rotation must not authorize
	// a session with a credential that was valid before the expensive hash.
	if err := h.Lock.Do(r.Context(), func() error {
		current, err := h.Applier.Load()
		if err != nil {
			return err
		}
		if !current.RequiresPassword() || current.Access.Password.Hash != cfg.Access.Password.Hash {
			core.WriteError(w, 401, "administrator password changed; sign in again")
			return nil
		}
		if err := h.Sessions.Issue(w, r, current.Access.Password); err != nil {
			return err
		}
		core.WriteJSON(w, 200, AccessView{Claimed: true, PasswordSet: true, Authenticated: true})
		return nil
	}); err != nil {
		core.WriteError(w, 503, err.Error())
	}
}

func (h HTTP) postLogout(w http.ResponseWriter, r *http.Request) {
	if err := h.Sessions.Logout(w, r); err != nil {
		core.WriteError(w, 503, err.Error())
		return
	}
	core.WriteJSON(w, 200, map[string]bool{"authenticated": false})
}

func (h HTTP) putPassword(w http.ResponseWriter, r *http.Request) {
	var req ClaimRequest
	if err := core.DecodeJSON(w, r, &req); err != nil {
		core.WriteError(w, 400, err.Error())
		return
	}
	password, err := h.passwordFor(r, req)
	if err != nil {
		core.WriteError(w, 400, err.Error())
		return
	}
	if err := h.Lock.Do(r.Context(), func() error {
		cfg, err := h.Applier.SetPassword(password)
		if err != nil {
			return err
		}
		if err := h.Sessions.RevokeAll(); err != nil {
			return err
		}
		core.WriteJSON(w, 200, AccessView{Claimed: cfg.Claimed(), PasswordSet: cfg.RequiresPassword(), Authenticated: !cfg.RequiresPassword()})
		return nil
	}); err != nil {
		core.WriteError(w, 503, err.Error())
	}
}

// localSource reports whether a request came from somewhere allowed to claim.
//
// Private, link-local and loopback ranges, per docs/system.md §4. The honest
// limits, stated rather than buried:
//
//   - It does not defend against a hostile device already on your LAN. `olr
//     claim --code` is the answer to that and is deliberately not the default,
//     because requiring it always recreates the "read a file over SSH and paste
//     it" friction v0.1.6 removed.
//   - It reads RemoteAddr and *not* X-Forwarded-For. A reverse proxy in front of
//     olr would make every request look local, so a header that any client can
//     set must never widen this. An operator who has put olr behind a proxy has
//     taken over deciding who reaches it, and can claim over the socket.
func localSource(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		// Includes the unix socket, whose RemoteAddr is not an IP at all. That
		// caller has already satisfied the socket's file mode, which outranks
		// anything this function could check.
		return host == "@" || host == "" || host == "unix"
	}
	addr = addr.Unmap()
	return addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast()
}

func (h HTTP) getMetrics(w http.ResponseWriter, r *http.Request) {
	proc := h.Proc
	if proc == "" {
		proc = "/proc"
	}
	metrics, err := readMetrics(proc)
	if err != nil {
		core.WriteError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	metrics.DiskUsed, metrics.DiskTotal = readRootDisk()
	// A pair of CPU readings measures a recent interval rather than lifetime load.
	first, err := readCPU(proc + "/stat")
	if err == nil {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(150 * time.Millisecond):
		}
		second, readErr := readCPU(proc + "/stat")
		if readErr == nil && second.total > first.total && second.idle >= first.idle {
			used := float64((second.total-first.total)-(second.idle-first.idle)) / float64(second.total-first.total) * float64(metrics.CPUCores)
			metrics.CPUUsedCores = &used
		}
	}
	core.WriteJSON(w, http.StatusOK, metrics)
}
