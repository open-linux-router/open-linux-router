// Package link owns which interfaces the operator has handed to olr, and the
// networks those interfaces carry.
//
// # What this is, and what it is not
//
// design.md §9 milestone 1 owes a `link` module that brings up interfaces,
// carries addressing, and lands the group object (§4.4) every module after it
// keys off. This is now most of that module:
//
//   - **Adoption** — the list of interface names the operator handed over. Pure
//     consent: storing it changes nothing on the box. It exists because the
//     adopt-only rule (§3.4, §7) was being enforced by three modules against a
//     fact that had nowhere to live, and a safety rule nobody can satisfy is
//     not a safety rule.
//   - **Groups** — the networks (§4.4). A group names a subnet and this box's
//     address on it, and applying one *does* reach the kernel. This is the half
//     that was missing, and its absence had a specific cost: `dhcp` could only
//     validate a range against whatever address an interface had been given
//     from outside olr, so an operator who wanted a different subnet met an
//     error that no page in olr could fix. olrd puts the address back when it
//     starts (Applier.Restore), because the kernel forgets it on a reboot and
//     nothing else on the box knows it.
//
// Still owed, and deliberately not invented here: bridges, VLANs, and taking an
// interface away from NetworkManager, systemd-networkd or ifupdown. The last of
// those has a cost design.md §7 states: until it lands, a member interface has
// two managers that do not know about each other, and only the operator can
// make them agree — docs/install.md says how. `Group.Members` is a list so that
// the day bridging lands the schema is already right, but Validate requires
// exactly one member until something exists that can create a bridge device.
//
// The observed half of an interface's facts — its current addresses, whether it
// is up — is still read from the kernel per request and never stored. There is
// no copy to drift (§4.5). What a group stores is *intent*: the subnet we want,
// not the subnet that is there. Apply is what closes the gap between them, and
// the difference between the two is drift rather than a second opinion.
package link

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/netip"
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

// MaxGroupNameLen bounds a network name.
//
// dnsmasq tags are built from it (`dhcp-range=set:<name>,...`), it appears in a
// URL path, and it is typed on a command line. 32 is comfortably more than any
// of those need and short enough that a list of them lines up in a terminal.
const MaxGroupNameLen = 32

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

	// Groups are the networks this box serves (design.md §4.4).
	//
	// Unlike Adopted, a group *is* configuration: it says what the subnet is
	// and what address this router holds on it, and applying one writes that
	// address to the kernel. That is the whole point — before groups existed
	// there was nowhere in olr to say "this LAN is 172.16.1.0/24", so `dhcp`
	// had to validate a range against whatever address the interface happened
	// to have been given from outside olr, and an operator who wanted a
	// different subnet met an error with no page behind it.
	Groups []Group `json:"groups,omitempty"`
}

// Group is one network: the object `dhcp`, `dns` and later `firewall` and
// `wifi` all key off (design.md §4.4).
//
// "Group" is the word the schema and the API use; **network** is the word the
// operator sees (`olr net add iot`). One object, one owner, and the second word
// is a label on the common path rather than a second model.
type Group struct {
	// Name is the primary key, and it is operator-chosen on purpose. `vlan30`
	// is an implementation detail; `guest` is the thing somebody named and the
	// thing that survives being moved to a different bridge.
	Name string `json:"name"`

	// Members are the adopted interfaces this network lives on.
	//
	// A list because §4.4 says a group has bridge members, and the day bridging
	// lands this field is already the right shape. Until then Validate requires
	// exactly one: more than one member needs a bridge device that something
	// has to create, and inventing the schema for that without building it
	// would be the retrofit §9 warns about in miniature.
	Members []string `json:"members"`

	// IPv4 is nil for a network that serves no IPv4 at all — which is a real
	// configuration now that `dhcp` can serve RA on its own, and was not
	// expressible before.
	IPv4 *GroupIPv4 `json:"ipv4,omitempty"`

	// IPv6 is deliberately absent. dnsmasq's `constructor:` derives the v6
	// prefix from the member's own address, so a delegated prefix that changes
	// is followed by the daemon rather than re-rendered by us — which is
	// everything §4.3's "v1 serves RA with SLAAC + RDNSS" needs. A ULA or a
	// static prefix would need olr to write a v6 address to the interface, and
	// that is a second kernel write path with its own failure modes. The field
	// goes here when it is built; nothing about this struct has to move.
}

// GroupIPv4 is a network's IPv4 addressing.
type GroupIPv4 struct {
	// Subnet is the network, as the operator thinks of it: 172.16.1.0/24.
	//
	// This is the field whose absence started all of this. It is stored intent,
	// not an observation, which is what lets `dhcp` validate a range against it
	// without consulting the kernel at all.
	Subnet netip.Prefix `json:"subnet"`

	// Router is this box's own address on the network. Nil derives the first
	// host address, which is `.1` on every ordinary prefix — design.md §11.2
	// makes that a requirement rather than a nicety, since `olr net add iot`
	// has to work with no flags at all.
	Router *netip.Addr `json:"router,omitempty"`
}

// RouterAddr resolves the stored or derived router address.
func (g GroupIPv4) RouterAddr() netip.Addr {
	if g.Router != nil && g.Router.IsValid() {
		return *g.Router
	}
	addr, _ := core.FirstHost(g.Subnet)
	return addr
}

// RouterPrefix is the router address with the network's mask — the form
// netlink wants when adding an address to an interface.
func (g GroupIPv4) RouterPrefix() netip.Prefix {
	return netip.PrefixFrom(g.RouterAddr(), g.Subnet.Bits())
}

// Group returns a network by name.
func (c Config) Group(name string) (Group, bool) {
	i := slices.IndexFunc(c.Groups, func(g Group) bool { return g.Name == name })
	if i < 0 {
		return Group{}, false
	}
	return c.Groups[i], true
}

// SetGroup adds or replaces a network, keyed by name.
func (c *Config) SetGroup(g Group) {
	if i := slices.IndexFunc(c.Groups, func(e Group) bool { return e.Name == g.Name }); i >= 0 {
		c.Groups[i] = g
		return
	}
	c.Groups = append(c.Groups, g)
	c.Normalize()
}

// RemoveGroup drops a network, reporting whether there was one.
//
// It does not check whether `dhcp` still references it. That is the same
// asymmetry Adopted has and it is deliberate: the module that stored the
// reference is the one that can explain what breaks, and it reports the
// problem against its own field. Guarding here would mean `link` importing
// every module that reads it, which is exactly the arrow §4.1 forbids.
func (c *Config) RemoveGroup(name string) bool {
	i := slices.IndexFunc(c.Groups, func(g Group) bool { return g.Name == name })
	if i < 0 {
		return false
	}
	c.Groups = slices.Delete(c.Groups, i, i+1)
	return true
}

// GroupFor returns the network an interface belongs to.
//
// The reverse lookup, for the surfaces that start from an interface row — the
// interface list has to say "ens18 carries lan", and without this it would have
// to walk every group itself.
func (c Config) GroupFor(iface string) (Group, bool) {
	for _, g := range c.Groups {
		if slices.Contains(g.Members, iface) {
			return g, true
		}
	}
	return Group{}, false
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
	} else {
		c.Adopted = out
	}

	c.normalizeGroups()
}

// normalizeGroups canonicalises the network list: names and members trimmed,
// members deduplicated, both sorted.
//
// The subnet is masked as well, so that `172.16.1.5/24` typed into a form is
// stored as `172.16.1.0/24`. Without that the same network could be written two
// ways, and every downstream byte comparison would call the difference drift.
func (c *Config) normalizeGroups() {
	out := make([]Group, 0, len(c.Groups))
	seen := map[string]bool{}
	for _, g := range c.Groups {
		g.Name = strings.TrimSpace(g.Name)
		if g.Name == "" || seen[g.Name] {
			continue
		}
		seen[g.Name] = true

		members := make([]string, 0, len(g.Members))
		member := map[string]bool{}
		for _, m := range g.Members {
			m = strings.TrimSpace(m)
			if m == "" || member[m] {
				continue
			}
			member[m] = true
			members = append(members, m)
		}
		slices.Sort(members)
		if len(members) == 0 {
			members = nil
		}
		g.Members = members

		if g.IPv4 != nil && g.IPv4.Subnet.IsValid() {
			v4 := *g.IPv4
			v4.Subnet = v4.Subnet.Masked()
			// An explicit router that equals what would be derived is dropped,
			// so the stored file says `.1` exactly once — in the subnet — and a
			// form that helpfully fills the field in does not turn a default
			// into a pin.
			if v4.Router != nil {
				if derived, ok := core.FirstHost(v4.Subnet); ok && *v4.Router == derived {
					v4.Router = nil
				}
			}
			g.IPv4 = &v4
		}

		out = append(out, g)
	}
	slices.SortStableFunc(out, func(a, b Group) int { return strings.Compare(a.Name, b.Name) })

	if len(out) == 0 {
		c.Groups = nil
		return
	}
	c.Groups = out
}

// Clone deep-copies the config, so a proposal can be diffed against the stored
// one without either sharing a backing array.
func (c Config) Clone() Config {
	out := Config{Adopted: slices.Clone(c.Adopted)}
	if c.Groups != nil {
		out.Groups = make([]Group, len(c.Groups))
		for i, g := range c.Groups {
			g.Members = slices.Clone(g.Members)
			if g.IPv4 != nil {
				v4 := *g.IPv4
				if v4.Router != nil {
					addr := *v4.Router
					v4.Router = &addr
				}
				g.IPv4 = &v4
			}
			out.Groups[i] = g
		}
	}
	return out
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

// Empty reports whether the module has been configured at all.
func (c Config) Empty() bool { return len(c.Adopted) == 0 && len(c.Groups) == 0 }

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
