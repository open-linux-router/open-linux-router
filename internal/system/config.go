// Package system owns the box itself rather than anything it forwards, and for
// now that means exactly one thing: who is allowed to configure it over the
// network.
//
// docs/system.md is the design. The short version, because it governs every
// file in here:
//
//	An unclaimed box cannot be configured over the network.
//
// A fresh install serves its web UI from the first second (design.md §7, as
// amended) and that is only safe because an unclaimed box refuses to be
// configured through it. The gate is in internal/daemon, beside the router
// table where authentication is applied; what lives here is the *state* it
// reads and the one route that changes it.
//
// design.md §10 #1 folds "who may log into olr" into this module as its
// `access` slice. That is all this is. There is no user model, no role, no
// account — one box, one optional password — and it is shaped so a later
// identity model replaces it rather than inheriting from it.
package system

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// ModuleName is the path segment, config section and event label for this
// module.
const ModuleName = "system"

// Config is the system module's intent.
type Config struct {
	// Access is nil until somebody claims this box.
	//
	// The nil-versus-set distinction is the whole design, so it is a pointer
	// rather than a value with a zero state. An absent section means *nobody
	// has decided*; a present one with no password means somebody decided to
	// have no password, on a screen that said what that meant. Those are
	// different facts and a zero value cannot tell them apart — which is
	// precisely the confusion v0.1.6 shipped, where a box with no password was
	// indistinguishable from a box nobody had configured.
	Access *Access `json:"access,omitempty"`
}

// Access records how an administrator chose to reach this box.
type Access struct {
	// ClaimedAt is when the choice was made.
	//
	// Stored because "has anybody set this box up" is a question asked months
	// later, by somebody who did not do it, and a timestamp answers it better
	// than the mere presence of a key. Not used for any decision.
	ClaimedAt time.Time `json:"claimed_at" jsonschema:"description=When this box was claimed."`

	// Password is the credential required on the TCP listener, or nil for none.
	//
	// nil is a legitimate, recorded answer rather than a missing one — see the
	// comment on Config.Access. docs/system.md §5 has the argument for offering
	// it at all; the short form is that a home LAN the operator already trusts
	// is the common case, and a product that refuses to allow it gets a
	// password of `router` on a sticky note instead.
	Password *Password `json:"password" jsonschema:"description=The password required over the network, or null for none."`
}

// Password is a stored credential.
//
// Only ever the derived form. There is no field here that could hold the
// password itself, which is the cheapest way to guarantee the document never
// grows one by accident.
type Password struct {
	// Hash is the PBKDF2 output, base64, with its parameters and salt.
	Hash string `json:"hash" jsonschema:"description=Derived form of the password. Never the password."`
}

// Claimed reports whether an administrator has recorded an access decision.
//
// The one question the daemon's gate asks, given a name so that the gate reads
// as the rule it implements rather than as a nil check.
func (c Config) Claimed() bool { return c.Access != nil }

// RequiresPassword reports whether the TCP listener should demand a credential.
func (c Config) RequiresPassword() bool {
	return c.Access != nil && c.Access.Password != nil
}

// UnmarshalConfig parses a config, rejecting unknown fields.
//
// Strict like every other module's decoder, and the stakes are higher here than
// most: a typo'd key inside `access` must not read as the *absence* of an
// access decision, because absence is what opens the claim route.
func UnmarshalConfig(data []byte) (Config, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()

	var c Config
	if err := dec.Decode(&c); err != nil {
		return Config{}, fmt.Errorf("parsing system config: %w", err)
	}
	return c, nil
}

// MarshalConfig renders this module's subtree of the document.
func MarshalConfig(c Config) ([]byte, error) { return json.Marshal(c) }

// FromDocument reads this module's subtree out of the configuration document.
//
// A document without a "system" key is not an error — it is what a box nobody
// has claimed looks like, which is every fresh install, and the gate needs that
// to be an ordinary answer rather than a failure. A daemon that could not tell
// "unclaimed" from "could not read the config" would have to choose between
// locking out the operator and opening up on a read error.
func FromDocument(d core.Document) (Config, error) {
	raw, ok := d.Raw(ModuleName)
	if !ok {
		return Config{}, nil
	}
	c, err := UnmarshalConfig(raw)
	if err != nil {
		return Config{}, fmt.Errorf("%s configuration: %w", ModuleName, err)
	}
	return c, nil
}
