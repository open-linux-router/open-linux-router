package dial

import (
	"fmt"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Applier is the read and write path onto the records.
//
// As thin as internal/link's, and for the same reason taken the same distance:
// this module has no backend, renders no file and drives no unit. Storing a
// record changes nothing on the box — what it changes is that olrd starts
// telling a third party this box's address on a timer, which is a consequence of
// the *publisher* picking the new config up (see HTTP.Watch), not of the write.
//
// So applying here is one atomic document write, and the plan's impact is none.
// That is honest about the machine and slightly understates the world: the first
// apply of a record is the moment this box starts talking to somebody it never
// talked to before. plan.go's warnings say so, because the plan preview is where
// an operator is looking when that becomes true.
type Applier struct {
	// Store is core's configuration document, which owns this module's intent
	// alongside every other module's.
	Store *core.Store

	// Links is the window onto adoption and addresses, used by validation and
	// by the plan. Nil is legal and skips the rules that need the box, which is
	// what keeps the module testable without one.
	Links LinkView
}

// Load reads stored intent out of the configuration document.
func (a Applier) Load() (Config, error) {
	doc, err := a.Store.Load()
	if err != nil {
		return Config{}, err
	}
	return FromDocument(doc)
}

// Save validates and stores intent, returning the stored form.
//
// Read-modify-write on the shared document, so a save here cannot drop another
// module's configuration. It is safe without further locking because every
// config write in the process holds the one global apply lock (§3.6) — the
// caller takes it.
func (a Applier) Save(cfg Config) (Config, error) {
	cfg.Normalize()
	if res := Validate(cfg, a.Links); !res.OK() {
		return cfg, res.Err()
	}

	doc, err := a.Store.Load()
	if err != nil {
		return cfg, err
	}
	data, err := MarshalConfig(cfg)
	if err != nil {
		return cfg, err
	}
	doc.Set(ModuleName, data)
	if err := a.Store.Save(doc); err != nil {
		return cfg, fmt.Errorf("storing configuration in %s: %w", a.Store.Path(), err)
	}
	return cfg, nil
}
