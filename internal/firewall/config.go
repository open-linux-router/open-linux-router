// Package firewall owns the `olr_filter` table: a default stance on what may
// reach this router, and the networks behind it, from outside.
//
// One switch and no rule list. docs/firewall.md has the argument; the short
// form is that a home router's firewall is strong because of its default, not
// because of how many rules it has, and every exception a house network needs
// is already written down somewhere else in olr — a port forward, a remote
// access server, a published service. So this module does not ask for them a
// second time. It reads them (Boundary) and opens exactly those, which means
// removing the thing closes its port with it and there is no rule anybody can
// forget to take out.
//
// What "outside" means is the one decision everything here rests on: **any
// interface that is not a member of one of `link`'s networks, and is not the
// dial-in tunnel.** Defined by what is trusted rather than by what is not, so
// that an interface olr has never heard of — a second NIC, a PPPoE session, a
// USB modem — is outside the moment it appears. Getting that wrong fails
// closed.
package firewall

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// ModuleName is this module's key in the document, its API prefix and its
// command.
const ModuleName = "firewall"

// Config is the stored intent. All of it.
type Config struct {
	// Enabled puts the default stance in force. Off by default: installing olr
	// changes nothing on a box until somebody says so (design.md §7), and this
	// is the module most able to cut an operator off from the box they are
	// configuring.
	Enabled bool `json:"enabled" jsonschema:"description=Block connections from outside that nothing here asked for."`
}

// UnmarshalConfig parses a stored section strictly.
func UnmarshalConfig(data []byte) (Config, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()

	var c Config
	if err := dec.Decode(&c); err != nil {
		return Config{}, fmt.Errorf("parsing firewall config: %w", err)
	}
	return c, nil
}

// MarshalConfig is the stored form.
func MarshalConfig(c Config) ([]byte, error) { return json.Marshal(c) }

// FromDocument reads this module's section. Absent means the zero Config,
// which is off.
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
