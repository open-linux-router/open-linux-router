package link

import (
	"fmt"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Applier is the read and write path onto adoption.
//
// Thinner even than internal/devices', and for the same reason taken one step
// further: this module has no backend *and* nothing it writes reaches the
// system. Adopting an interface stores a sentence of consent. It renders no
// file, reloads no daemon, and touches no link — which is exactly what §7's
// promise that "install alone changes nothing" needs, and why applying here is
// one atomic document write with an impact of none.
//
// What adoption *does* is unlock the other modules: `dhcp`, `dns` and `gateway`
// each refuse an interface nobody handed them. So the visible effect of an
// adopt is that a form somewhere else stops rejecting you, and the visible
// effect of a release is that a pool on that interface stops validating. That
// asymmetry is the reason the UI warns before a release rather than after.
type Applier struct {
	// Store is core's configuration document, which owns this module's intent
	// alongside every other module's.
	Store *core.Store

	// Source reads the observed half. Nil means Kernel.
	Source Source
}

// Load reads stored intent out of the configuration document.
func (a Applier) Load() (Config, error) {
	doc, err := a.Store.Load()
	if err != nil {
		return Config{}, err
	}
	return FromDocument(doc)
}

// Observe reads the kernel's interfaces.
func (a Applier) Observe() ([]Interface, error) {
	if a.Source == nil {
		return Kernel()
	}
	return a.Source()
}

// observeQuietly reads the interfaces for validation, tolerating failure.
//
// A machine whose interface list cannot be read is not a reason to refuse to
// store consent: every rule that needs the list produces warnings, not errors,
// and the alternative is an operator locked out of adopting anything by a
// transient netlink failure. The name rules still apply, because they are the
// ones that can actually make the document meaningless.
func (a Applier) observeQuietly() []Interface {
	observed, err := a.Observe()
	if err != nil {
		return nil
	}
	return observed
}

// Save validates and stores intent, returning the stored form.
//
// Read-modify-write on the shared document, so a save here cannot drop another
// module's configuration. It is safe without further locking because every
// config write in the process holds the one global apply lock (§3.6) — the
// caller takes it.
func (a Applier) Save(cfg Config) (Config, error) {
	cfg.Normalize()
	if res := Validate(cfg, a.observeQuietly()); !res.OK() {
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
