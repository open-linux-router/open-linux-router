package system

import (
	"errors"
	"fmt"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Applier reads and writes the access decision.
//
// The thinnest applier in the tree, and structurally so rather than because it
// is unfinished: this module drives no daemon, renders no file and programs no
// kernel table. There is nothing to converge. What it owns is one fact in the
// document, and the only reason it is an Applier at all is that every other
// module's HTTP surface takes one and consistency is worth more here than
// saving a struct.
type Applier struct {
	// Store is core's configuration document.
	Store *core.Store

	// Now is injectable so that a test can assert on the recorded timestamp
	// rather than on "something close to now". Nil means time.Now.
	Now func() time.Time
}

// ErrAlreadyClaimed is returned when a claim arrives at a box that has one.
//
// Its own error because the HTTP layer answers it with 409 rather than 400: the
// request was well-formed and would have been correct a moment earlier, and
// that distinction is what lets the UI tell "somebody else just claimed this
// box" apart from "you sent nonsense".
var ErrAlreadyClaimed = errors.New("this box has already been claimed")

// ErrNotClaimed is returned when an access change arrives before a claim.
var ErrNotClaimed = errors.New("this box has not been claimed yet")

// Load reads the stored access decision.
func (a Applier) Load() (Config, error) {
	doc, err := a.Store.Load()
	if err != nil {
		return Config{}, err
	}
	return FromDocument(doc)
}

// Claim records the first access decision, and refuses to record a second.
//
// The refusal is the point, and it is why this is not simply Save. Claiming is
// the one privileged act reachable without a credential (docs/system.md §4), so
// it has to be usable exactly once — otherwise a box with a password could be
// re-claimed without one, and the password would be decoration.
//
// password may be nil, which records the deliberate choice of none.
func (a Applier) Claim(password *Password) (Config, error) {
	return a.update(func(cfg *Config) error {
		if cfg.Claimed() {
			return ErrAlreadyClaimed
		}
		cfg.Access = &Access{ClaimedAt: a.now(), Password: password}
		return nil
	})
}

// SetPassword changes the credential on a box that is already claimed.
//
// nil removes it. Separate from Claim because the authorisation is different:
// this one is reached through whatever the box currently requires, while Claim
// is reached by anybody who gets there first. Collapsing them into one route
// would mean the unauthenticated path could also change a password.
func (a Applier) SetPassword(password *Password) (Config, error) {
	return a.update(func(cfg *Config) error {
		if !cfg.Claimed() {
			return ErrNotClaimed
		}
		cfg.Access.Password = password
		return nil
	})
}

// update is read-modify-write against the document, the same shape every other
// module's Save has.
//
// The read and the write are two operations and the window between them is
// closed by core's apply lock, which the HTTP layer holds across a mutating
// route (§3.6) — not by anything here. Worth naming because Claim's
// once-only rule looks like it needs a transaction and does not: the only
// caller that can race it is another request, and those are serialised above.
func (a Applier) update(change func(*Config) error) (Config, error) {
	doc, err := a.Store.Load()
	if err != nil {
		return Config{}, err
	}
	cfg, err := FromDocument(doc)
	if err != nil {
		return Config{}, err
	}
	if err := change(&cfg); err != nil {
		return cfg, err
	}

	raw, err := MarshalConfig(cfg)
	if err != nil {
		return cfg, err
	}
	doc.Set(ModuleName, raw)
	if err := a.Store.Save(doc); err != nil {
		return cfg, fmt.Errorf("storing configuration in %s: %w", a.Store.Path(), err)
	}
	return cfg, nil
}

func (a Applier) now() time.Time {
	if a.Now == nil {
		return time.Now().UTC()
	}
	return a.Now().UTC()
}
