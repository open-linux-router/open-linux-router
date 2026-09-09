// Package link owns which interfaces the operator has handed to olr.
//
// # What this is, and what it is not
//
// design.md §9 milestone 1 owes a `link` module that brings up interfaces,
// carries addressing, and lands the group object (§4.4) every module after it
// keys off. This is not that module. It owns exactly one field — the list of
// adopted interface names — and it exists because the adopt-only rule (§3.4,
// §7) was being enforced by three modules against a fact that had nowhere to
// live: `dhcp`, `dns` and `gateway` all refuse an interface the operator did
// not hand over, `olr adopt` was a stub, and the flag was reachable only by
// hand-writing a JSON file that olrd was never told to read. A safety rule
// nobody can satisfy is not a safety rule.
//
// So this is the smallest thing that makes that rule real, and it is
// deliberately shaped so the real module can absorb it rather than collide with
// it:
//
//   - It stores adoption and nothing else. No addressing, no bring-up, no
//     taking an interface from NetworkManager. Those are §7's other half and
//     they need the real module.
//   - It invents no primary key. Pools stay keyed by kernel interface name,
//     exactly as they were, so the retrofit design.md §9 warns about — moving
//     the key to a group — is neither done here nor made harder.
//   - The other half of an interface's facts, its addresses and whether it is
//     up, is read from the kernel per request and never stored. There is no
//     copy to drift (§4.5), which is the one thing the hand-written links file
//     could not promise.
package link

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// ModuleName is the path segment, config section and event label for this
// module.
const ModuleName = "link"

// MaxInterfaceNameLen is the kernel's IFNAMSIZ minus the terminator. A longer
// name cannot name an interface that exists, so accepting one would only defer
// the complaint to a place with less context.
const MaxInterfaceNameLen = 15

// Config is the link module's intent.
//
// One field, and it is a list of names rather than a list of objects. That is a
// bet worth stating: the day this grows an address or a group it becomes a list
// of structs, and the migration is a shim in UnmarshalConfig. Making it a struct
// today to avoid that would mean inventing the field set §10 decision 7 says is
// still open, in the module least able to change it later.
type Config struct {
	// Adopted names the interfaces the operator has handed to olr.
	//
	// Adoption is consent, not configuration: it changes nothing on the box by
	// itself. What it does is let the other modules act on an interface — a
	// pool, a resolver, an exit — which is why removing a name here can make
	// another module's stored config invalid. That is reported by that module,
	// against its own field, rather than guarded here; see Validate.
	Adopted []string `json:"adopted,omitempty"`
}

// Normalize puts the config in canonical form: trimmed, deduplicated, sorted.
//
// Not lowercased. Interface names are case-sensitive on Linux, so `eth0` and
// `ETH0` are two different interfaces and folding them would silently adopt one
// the operator did not name.
//
// Sorting matters for the same reason it does in every other module: the
// document is compared as bytes downstream, so a reordered array would read as
// a change nobody made.
func (c *Config) Normalize() {
	out := make([]string, 0, len(c.Adopted))
	seen := map[string]bool{}
	for _, name := range c.Adopted {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	slices.Sort(out)

	// nil rather than an empty slice, so that a config with nothing adopted
	// marshals as an absent key rather than `"adopted": []`. The two mean the
	// same thing and only one of them is worth writing to a file.
	if len(out) == 0 {
		c.Adopted = nil
		return
	}
	c.Adopted = out
}

// Clone deep-copies the config, so a proposal can be diffed against the stored
// one without either sharing a backing array.
func (c Config) Clone() Config {
	return Config{Adopted: slices.Clone(c.Adopted)}
}

// IsAdopted reports whether the operator handed us this interface.
func (c Config) IsAdopted(name string) bool {
	return slices.Contains(c.Adopted, strings.TrimSpace(name))
}

// Adopt adds an interface, reporting whether anything changed.
//
// Idempotent, and that is load-bearing rather than tidy: `olr adopt eth0` run
// twice should be a no-op with a calm message, not an error the operator has to
// read carefully to discover was harmless.
func (c *Config) Adopt(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || c.IsAdopted(name) {
		return false
	}
	c.Adopted = append(c.Adopted, name)
	c.Normalize()
	return true
}

// Release drops an interface, reporting whether it was there.
func (c *Config) Release(name string) bool {
	name = strings.TrimSpace(name)
	i := slices.Index(c.Adopted, name)
	if i < 0 {
		return false
	}
	c.Adopted = slices.Delete(c.Adopted, i, i+1)
	c.Normalize()
	return true
}

// Empty reports whether anything has been adopted at all.
func (c Config) Empty() bool { return len(c.Adopted) == 0 }

// UnmarshalConfig parses a config, rejecting unknown fields.
//
// Strict for the reason internal/dhcp/config.go gives: a typo'd key that is
// silently ignored produces a box that is quietly not doing what its config
// says, and here it would produce one that quietly adopted nothing.
func UnmarshalConfig(data []byte) (Config, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()

	var c Config
	if err := dec.Decode(&c); err != nil {
		return Config{}, fmt.Errorf("parsing link config: %w", err)
	}
	c.Normalize()
	return c, nil
}

// MarshalConfig renders this module's subtree of the document.
//
// No "$schema" key: the document has exactly one, written by the store, and a
// second nested inside a module's subtree would be a claim this module is not
// in a position to make. Indentation is likewise the store's — it re-indents
// the whole document in one pass.
func MarshalConfig(c Config) ([]byte, error) {
	c.Normalize()
	return json.Marshal(c)
}

// FromDocument reads this module's subtree out of the configuration document.
//
// A document without a "link" key is not an error — it means nothing has been
// adopted yet, which is exactly what a fresh install looks like and what the
// interface list must render as an empty state rather than a failure.
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
