package devices

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
const ModuleName = "devices"

// Config is the devices module's intent: the *identity* half of a device
// (design.md §4.4).
//
// Identity is ours, stored and revisioned; presence — online, current address,
// last seen — is read through the source that observes it and never stored as
// truth. That split is the whole design of this module. Anything in this struct
// is something a human decided; anything observed lives in Sighting and is
// stamped with an as_of instead.
//
// This is also why the module owns no daemon. There is nothing to render and no
// service to reload: a rename changes a label on a screen, not a packet on the
// wire. §10 decision 6 asked who owns the inventory, and the answer this
// module implements is "a foundation module of its own", so that `firewall` and
// `qos` can reference a laptop without depending on `dhcp`.
type Config struct {
	// Devices is keyed by MAC. Sorted canonically by Normalize, so a UI that
	// re-reads after a write finds rows where it left them.
	Devices []Device `json:"devices,omitempty"`
}

// Device is what a human has said about one client on the network.
//
// Every field is optional except the key. A device with nothing but a MAC is
// still worth storing: it is how a statically-addressed printer that never
// speaks DHCP gets into the list at all (§10 decision 7).
type Device struct {
	// MAC is the identity. Canonicalised by core.NormalizeMAC so that this and
	// a dhcp reservation for the same hardware are the same string.
	MAC string `json:"mac"`

	// Name is what the operator calls it. Absent means the UI falls back to an
	// observed hostname, and then to the MAC — a device is never nameless on
	// screen, but we do not silently promote a hostname the client chose into
	// stored intent the operator did not.
	Name string `json:"name,omitempty"`

	// Category is the operator's answer, and it beats detection unconditionally
	// (icon-style-spec.md resolution order). Unset means detection may answer.
	//
	// This is the field that makes the icon *identity* rather than presence: a
	// picture the operator corrected must not be silently changed back by the
	// next fingerprint update.
	Category Category `json:"category,omitempty"`

	// Model names a specific product, e.g. "synology/ds224plus", and selects a
	// tier-2 image where one exists. Validated as a strict slug: it addresses
	// an asset, so a value with a slash-dot in it would be a path traversal
	// waiting for the day we serve these from disk.
	Model string `json:"model,omitempty"`

	// Notes is free text for the operator's own benefit — "in the loft", "belongs
	// to the upstairs tenant". Never parsed.
	Notes string `json:"notes,omitempty"`
}

// MaxNameLen and the limits below exist so that a UI can lay out a row without
// defending against a megabyte in a name field. They are generous enough that
// no legitimate value hits them.
const (
	MaxNameLen  = 64
	MaxModelLen = 128
	MaxNotesLen = 512
)

// UnmarshalConfig parses a document strictly.
//
// Unknown fields are rejected for the reason internal/dhcp/http.go gives on the
// PUT path: a mistyped key that silently did nothing would be the worst
// outcome — a 200, an operator who believes the setting took, and a screen that
// disagrees.
func UnmarshalConfig(data []byte) (Config, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()

	var c Config
	if err := dec.Decode(&c); err != nil {
		return Config{}, fmt.Errorf("parsing devices config: %w", err)
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
// A document without a "devices" key is not an error — it means nobody has
// named anything yet, which is exactly what a fresh install looks like and what
// the list must render as an empty state rather than a failure.
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

// Normalize puts the config in canonical form: MACs canonicalised, whitespace
// trimmed, devices sorted by MAC.
//
// Sorting is not cosmetic. The list is rendered in stored order, so an
// append-on-edit would make a row jump to the bottom the moment it was renamed
// — which is a bug the dhcp reservation and pool tables currently have. Sorting
// on the way in means position is a function of identity, not of edit history.
//
// It deliberately does not validate; a malformed MAC is left as-is for Validate
// to report against a proper path.
func (c *Config) Normalize() {
	for i, d := range c.Devices {
		if mac, err := core.NormalizeMAC(d.MAC); err == nil {
			c.Devices[i].MAC = mac
		}
		c.Devices[i].Name = strings.TrimSpace(c.Devices[i].Name)
		c.Devices[i].Model = strings.ToLower(strings.TrimSpace(c.Devices[i].Model))
		c.Devices[i].Notes = strings.TrimSpace(c.Devices[i].Notes)
	}
	slices.SortStableFunc(c.Devices, func(a, b Device) int {
		return strings.Compare(a.MAC, b.MAC)
	})
}

// Find returns the stored identity for a MAC, or false.
func (c Config) Find(mac string) (Device, bool) {
	norm, err := core.NormalizeMAC(mac)
	if err != nil {
		return Device{}, false
	}
	for _, d := range c.Devices {
		if d.MAC == norm {
			return d, true
		}
	}
	return Device{}, false
}

// Upsert adds a device or replaces the one with the same MAC.
func (c *Config) Upsert(d Device) {
	if mac, err := core.NormalizeMAC(d.MAC); err == nil {
		d.MAC = mac
	}
	for i := range c.Devices {
		if c.Devices[i].MAC == d.MAC {
			c.Devices[i] = d
			c.Normalize()
			return
		}
	}
	c.Devices = append(c.Devices, d)
	c.Normalize()
}

// Remove drops a device's stored identity, reporting whether it was there.
//
// The device does not leave the list: if it still holds a lease or answers ARP
// it reappears on the next read with a detected category and no name. Forgetting
// what we were told about a device is not the same as pretending it is gone, and
// conflating the two would make "remove" look like it had disconnected something.
func (c *Config) Remove(mac string) bool {
	norm, err := core.NormalizeMAC(mac)
	if err != nil {
		return false
	}
	for i, d := range c.Devices {
		if d.MAC == norm {
			c.Devices = append(c.Devices[:i], c.Devices[i+1:]...)
			return true
		}
	}
	return false
}

// Empty reports whether anything has been stored at all.
func (c Config) Empty() bool { return len(c.Devices) == 0 }
