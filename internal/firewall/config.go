// Package firewall is named for more than it currently does.
//
// design.md §4 gives a module called `firewall` the whole of "nftables
// olr_filter, olr_nat — zones, rules, NAT, forwards". This is that module with
// one of those four built: it owns a **port forward**, and it has no zones, no
// rule list and no filtering policy at all.
//
// The name is kept anyway, for the reason internal/link keeps its own: the word
// is already the one design.md reserves for this territory, and renaming the
// package the week filtering lands would be a rename nobody learns anything
// from. What the module is for is stated here, and docs/firewall.md §8 is the
// scope contract.
//
// The consequence worth knowing before reading any of this: **olr does not
// maintain a default-deny stance on forwarded traffic**, so creating a forward
// does not open a hole in a firewall of ours — there is not one. It installs a
// NAT translation. If something else on the box filters the forward hook, that
// filter still applies, and docs/firewall.md §5.2 is why we report that rather
// than trying to overrule it.
//
// The operator meets one sentence: *something on the internet connects to this
// router on a port, and the connection is delivered to one device on your
// network instead.*
package firewall

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"slices"
	"strings"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// ModuleName is the path segment, config section and event label for this
// module.
const ModuleName = "firewall"

// Config is the firewall module's intent — what the operator asked for, not
// what is running. It is the single source for the CLI flags, the REST body,
// the UI form and the MCP tool definition (design.md §3.2 rule 3), so a field
// added here appears on every surface without further work.
//
// Fields without `omitempty` are reflected as schema-required (design.md §10,
// config format), so the tags are load-bearing.
type Config struct {
	// Enabled controls whether any of this is programmed into the kernel at
	// all. Disabling removes our table and leaves the box translating exactly
	// as it did before olr was installed — which is also what makes the module
	// safe to try (design.md §7).
	//
	// Keeping the configuration while off means it can be turned back on
	// without retyping it, matching internal/dhcp, internal/dns and
	// internal/gateway.
	Enabled bool `json:"enabled"`

	// Forwards are the standing instructions. Empty means nothing from outside
	// reaches anything inside, which is what an unconfigured box already does.
	Forwards []Forward `json:"forwards,omitempty"`
}

// Forward is one standing instruction: connections arriving on this interface,
// on this port, go to that address and port instead.
//
// Flat and independent by design. There is no ladder and no inheritance here,
// unlike internal/gateway's assignments, and docs/firewall.md §2 has the
// argument: "where does my phone go?" has an answer whether or not anybody set
// one, while "what reaches my NAS from outside?" is *nothing* until somebody
// writes it down — so the list of what they wrote down is the whole model.
type Forward struct {
	// Name is what the operator calls it — "web", "minecraft", "vpn". It is the
	// identity every surface refers to it by, so renaming is a rename
	// everywhere; see Config.Rename.
	Name string `json:"name"`

	// In is the interface connections arrive on.
	//
	// An interface rather than "the internet", and that is not a placeholder
	// for a better word: a box can have two uplinks, and a forward that
	// silently applied to both would be a policy nobody wrote. It becomes a
	// network name when link grows groups (docs/gateway.md §2.5's staging), and
	// the stored document does not change shape when it does.
	In string `json:"in"`

	// Protocol is tcp, udp or both. Empty means tcp.
	Protocol Protocol `json:"protocol,omitempty"`

	// Port is the port, or ports, connections arrive on.
	Port PortRange `json:"port"`

	// To is where they go instead: an address on this network and the port to
	// deliver to.
	//
	// An address and not a reference to a device in the inventory, and
	// docs/firewall.md §1.2 is the argument. A forward is a kernel rule that
	// has to exist at boot, before any lease has been handed out and while the
	// device is switched off, so a device reference would either leave the port
	// dead until the device appeared or be resolved against a remembered
	// address — which is an address with one more place to disagree. The UI
	// offers a device picker that fills this in and warns when that device has
	// no fixed address; what is stored is the address either way.
	//
	// When Port is a range, this names the first port of the same-width range
	// inside, and Validate insists it *is* the same range — see PortRange and
	// docs/firewall.md §1.3.
	To netip.AddrPort `json:"to"`

	// Hairpin makes the forward work from inside the network too (§4). Nil
	// means on.
	//
	// A pointer because the default is *on* and a plain bool could not tell
	// "the operator turned it off" from "the field was never written" — the
	// distinction that decides whether a stored document from an older olr
	// keeps behaving the way it did.
	//
	// On by default because off-by-default is a setting nobody discovers: what
	// it prevents does not present as a missing feature, it presents as the
	// port forward being broken, tested from the one machine the operator has
	// to hand. What it costs is that the device sees the router's address
	// rather than the real client's, which Validate warns about every time.
	Hairpin *bool `json:"hairpin,omitempty"`

	// Slot is the only field an operator never sets: it is allocated on save
	// and preserved thereafter.
	//
	// It exists for one thing — the named counter (docs/firewall.md §3.4) —
	// and it is stored rather than derived for internal/gateway/resources.go's
	// reason, one size down. Deriving it from position in a sorted list means
	// adding a forward called "backup" renumbers "web", and the number an
	// operator is watching to answer *has this ever been hit?* resets because
	// of an unrelated row. It is also the only spelling of a counter name that
	// is certainly a legal nftables identifier, which an operator-chosen name
	// is not.
	Slot int `json:"slot"`
}

// MaxNameLen bounds a forward's name so a UI can lay out a row without
// defending against a megabyte in a field.
const MaxNameLen = 64

// HairpinOrDefault resolves the nil pointer: on.
func (f Forward) HairpinOrDefault() bool {
	if f.Hairpin == nil {
		return true
	}
	return *f.Hairpin
}

// ProtocolOrDefault resolves the empty protocol.
//
// tcp, because it is the overwhelming majority of what anybody forwards and
// because the alternative — refusing a forward that did not say — would make
// the common command longer to serve the rare one. `olr firewall show forwards`
// prints the column, so a forward that should have been udp is visible rather
// than mysterious.
func (f Forward) ProtocolOrDefault() Protocol {
	if f.Protocol == "" {
		return ProtocolTCP
	}
	return f.Protocol
}

// Find returns the forward with this name, or false.
func (c Config) Find(name string) (Forward, bool) {
	for _, f := range c.Forwards {
		if f.Name == name {
			return f, true
		}
	}
	return Forward{}, false
}

// Empty reports whether anything has been configured at all.
func (c Config) Empty() bool { return len(c.Forwards) == 0 }

// UnmarshalConfig parses a document strictly.
//
// Unknown fields are rejected for the reason internal/dhcp gives on the PUT
// path: a mistyped key that silently did nothing would be the worst outcome — a
// 200, an operator who believes the setting took, and a screen that disagrees.
func UnmarshalConfig(data []byte) (Config, error) {
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()

	var c Config
	if err := dec.Decode(&c); err != nil {
		return Config{}, fmt.Errorf("parsing firewall config: %w", err)
	}
	c.Normalize()
	return c, nil
}

// MarshalConfig renders intent for the store.
func MarshalConfig(c Config) ([]byte, error) {
	c.Normalize()
	return json.MarshalIndent(c, "", "  ")
}

// FromDocument reads this module's subtree out of the configuration document.
//
// A document without a "firewall" key is not an error — it means nobody has
// forwarded a port, which is exactly what a fresh install looks like and what
// the screen must render as an empty state rather than a failure.
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

// Normalize puts the config in canonical form: names and interfaces trimmed,
// forwards sorted, slots allocated.
//
// Sorting is not cosmetic. The list is rendered in stored order, so an
// append-on-edit would make a row jump to the bottom the moment it was renamed
// — the bug internal/devices called out in the dhcp tables. Sorting on the way
// in means position is a function of identity, not of edit history.
//
// It deliberately does not validate: a malformed forward is left as-is for
// Validate to report against a proper path.
func (c *Config) Normalize() {
	for i := range c.Forwards {
		c.Forwards[i].Name = strings.TrimSpace(c.Forwards[i].Name)
		c.Forwards[i].In = strings.TrimSpace(c.Forwards[i].In)
		c.Forwards[i].Protocol = Protocol(strings.TrimSpace(string(c.Forwards[i].Protocol)))
	}

	slices.SortStableFunc(c.Forwards, func(a, b Forward) int {
		return strings.Compare(a.Name, b.Name)
	})

	// Last, because it depends on the names being settled and it is the one
	// step here that invents a value rather than tidying one.
	c.allocateSlots()
}

// Upsert adds a forward or replaces the one with the same name, keeping its
// slot.
//
// Keeping the slot is the point of the method existing at all: an edit that
// silently reallocated it would reset the counter somebody is watching to
// decide whether the forward is being hit.
func (c *Config) Upsert(f Forward) {
	for i := range c.Forwards {
		if c.Forwards[i].Name == f.Name {
			if f.Slot == 0 {
				f.Slot = c.Forwards[i].Slot
			}
			c.Forwards[i] = f
			c.Normalize()
			return
		}
	}
	c.Forwards = append(c.Forwards, f)
	c.Normalize()
}

// Remove drops a forward, reporting whether it was there.
func (c *Config) Remove(name string) bool {
	for i, f := range c.Forwards {
		if f.Name == name {
			c.Forwards = append(c.Forwards[:i], c.Forwards[i+1:]...)
			return true
		}
	}
	return false
}

// Rename changes a forward's name, keeping its slot and so its counter.
//
// Nothing else in this module references a forward by name — there is no
// Default and no assignment list, which is the one way this object is simpler
// than internal/gateway's exit. So this is a rename in one place, and it exists
// as a method rather than as two edits only so that the slot survives it.
func (c *Config) Rename(from, to string) bool {
	f, ok := c.Find(from)
	if !ok {
		return false
	}
	f.Name = to
	c.Remove(from)
	c.Forwards = append(c.Forwards, f)
	c.Normalize()
	return true
}
