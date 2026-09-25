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

	// Groups are the operator's sets of devices — "Serving", "Personal", "IoT"
	// (design.md §4.4). A device is in at most one, named by Device.Group.
	//
	// Nothing is seeded and nothing is assigned automatically. A group exists
	// because somebody made it and a device is in one because somebody put it
	// there; a list of groups we invented, filled by rules the operator never
	// wrote, would be a second taxonomy competing with the category for no
	// reason they asked for.
	Groups []Group `json:"groups,omitempty"`
}

// Group is one set of devices.
//
// Today it carries only a name and is used to lay out the network map. It is a
// struct rather than a string because it is the tier gateway.md §2.1 reserves
// between a network and a device: the day a group carries a policy, that
// policy is owned by the module that enforces it and references the group by
// name, the way gateway references a network — nothing about this shape has to
// move for that.
type Group struct {
	// Name is the key, operator-chosen, and renamable: RenameGroup carries
	// every member along, so a rename is never a group that quietly emptied.
	Name string `json:"name"`
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

	// Group names the one group this device is in, or is empty for none.
	//
	// Stored on the device rather than as a member list on the group, and that
	// is what makes membership exclusive by construction: a device has one
	// field, so it cannot be in two groups, and there is no conflict for any
	// later policy to refuse (gateway.md §2.3).
	Group string `json:"group,omitempty"`
}

// MaxNameLen and the limits below exist so that a UI can lay out a row without
// defending against a megabyte in a name field. They are generous enough that
// no legitimate value hits them.
const (
	MaxNameLen      = 64
	MaxModelLen     = 128
	MaxNotesLen     = 512
	MaxGroupNameLen = 32
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
		c.Devices[i].Group = strings.TrimSpace(c.Devices[i].Group)
	}
	slices.SortStableFunc(c.Devices, func(a, b Device) int {
		return strings.Compare(a.MAC, b.MAC)
	})

	// Groups are trimmed and sorted by name, for the same reason devices are
	// sorted by MAC. Duplicates are left in place for Validate to name: two
	// groups called "IoT" is something the operator typed, and silently keeping
	// one would drop whatever made them type the other.
	for i := range c.Groups {
		c.Groups[i].Name = strings.TrimSpace(c.Groups[i].Name)
	}
	slices.SortStableFunc(c.Groups, func(a, b Group) int {
		return strings.Compare(a.Name, b.Name)
	})
	if len(c.Groups) == 0 {
		c.Groups = nil
	}
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

// Clone copies the config so an edit can be diffed against the stored one
// without the two sharing a backing array. Every field of Device and Group is a
// string, so copying the slices is a deep copy.
func (c Config) Clone() Config {
	return Config{Devices: slices.Clone(c.Devices), Groups: slices.Clone(c.Groups)}
}

// FindGroup returns a group by name.
func (c Config) FindGroup(name string) (Group, bool) {
	i := slices.IndexFunc(c.Groups, func(g Group) bool { return g.Name == name })
	if i < 0 {
		return Group{}, false
	}
	return c.Groups[i], true
}

// UpsertGroup adds a group or replaces the one with the same name.
func (c *Config) UpsertGroup(g Group) {
	if i := slices.IndexFunc(c.Groups, func(e Group) bool { return e.Name == g.Name }); i >= 0 {
		c.Groups[i] = g
		return
	}
	c.Groups = append(c.Groups, g)
	c.Normalize()
}

// RenameGroup changes a group's name and every member's reference to it,
// together, reporting whether the group existed.
//
// Together is the reason this is a method: membership is stored on the device,
// so renaming only the group would leave every member pointing at a name that
// no longer exists — a rename that looked like it emptied the group.
func (c *Config) RenameGroup(from, to string) bool {
	i := slices.IndexFunc(c.Groups, func(g Group) bool { return g.Name == from })
	if i < 0 {
		return false
	}
	c.Groups[i].Name = to
	for j := range c.Devices {
		if c.Devices[j].Group == from {
			c.Devices[j].Group = to
		}
	}
	c.Normalize()
	return true
}

// RemoveGroup drops a group and takes its members out of it, reporting whether
// it existed.
//
// Members are released rather than the delete refused, which is the opposite of
// what gateway does for an exit still in use. The difference is what a group
// carries: today it only arranges the map, so emptying it disconnects nothing
// and the devices simply show as ungrouped. The day a group carries a policy,
// deleting one changes where its members' traffic goes, and this should refuse
// the way gateway's Remove does.
func (c *Config) RemoveGroup(name string) bool {
	i := slices.IndexFunc(c.Groups, func(g Group) bool { return g.Name == name })
	if i < 0 {
		return false
	}
	c.Groups = slices.Delete(c.Groups, i, i+1)
	for j := range c.Devices {
		if c.Devices[j].Group == name {
			c.Devices[j].Group = ""
		}
	}
	c.Normalize()
	return true
}

// SetDeviceGroup puts a device in a group, or takes it out of every group when
// group is empty.
//
// A device that has never been described gets an entry holding just its MAC
// and the group, which is how a device that was only ever seen joins one — it
// is the same "a MAC alone is worth storing" rule the printer relies on.
func (c *Config) SetDeviceGroup(mac, group string) {
	d, _ := c.Find(mac)
	if d.MAC == "" {
		d.MAC = mac
	}
	d.Group = group
	c.Upsert(d)
}

// Empty reports whether anything has been stored at all.
func (c Config) Empty() bool { return len(c.Devices) == 0 && len(c.Groups) == 0 }
