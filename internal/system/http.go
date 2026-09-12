package system

import (
	"errors"
	"net"
	"net/http"
	"net/netip"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// HTTP is this module's REST surface.
type HTTP struct {
	Applier Applier
	Lock    *core.Lock
	Events  *core.Events
}

// Routes is the module's surface, declared as data so it can be enumerated
// rather than only served.
func (h HTTP) Routes() []core.Route {
	return []core.Route{
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
	PasswordSet bool `json:"password_set"`
}

func (h HTTP) getAccess(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.Applier.Load()
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	core.WriteJSON(w, http.StatusOK, AccessView{
		Claimed:     cfg.Claimed(),
		PasswordSet: cfg.RequiresPassword(),
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

	switch {
	case req.Password != "":
		// Refused rather than stored, and this is a deliberate release
		// boundary rather than an oversight. Nothing enforces a password yet
		// and no screen collects one, so accepting it here would leave a
		// credential in the document that either does nothing — a stored lie —
		// or locks the operator out of a box with no login form to get back in
		// through. The document shape is already decided (Access.Password), so
		// turning this on is additive.
		core.WriteError(w, http.StatusNotImplemented,
			"passwords are not supported yet; claim with no_password and set one when "+
				"the next release adds the login screen",
			core.Problem{Path: "password", Message: "not supported yet"})
		return

	case !req.NoPassword:
		core.WriteError(w, http.StatusBadRequest,
			"set no_password to claim this router",
			core.Problem{Path: "no_password", Message: "must be true"})
		return
	}

	// nil: the recorded choice of no password (docs/system.md §2), which is a
	// decision rather than a default.
	cfg, err := h.Applier.Claim(nil)
	switch {
	case errors.Is(err, ErrAlreadyClaimed):
		core.WriteError(w, http.StatusConflict, err.Error())
		return
	case err != nil:
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	core.WriteJSON(w, http.StatusOK, AccessView{
		Claimed:     cfg.Claimed(),
		PasswordSet: cfg.RequiresPassword(),
	})
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
		return true
	}
	addr = addr.Unmap()
	return addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast()
}
