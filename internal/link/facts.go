package link

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"slices"
	"strings"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// The join: stored consent against observed facts.
//
// This is the same shape internal/devices/list.go has for a device — an
// operator's half that is ours and stored, an observed half that is read
// through whatever observes it — and it is the only place the two meet, so
// "adoption is intent, addresses are facts" is a property of the system rather
// than something every caller has to remember.

// ErrNoSuchInterface is returned by Facts.Interface for a name the kernel does
// not have and nobody has adopted.
//
// The three consumer modules each declare their own copy of this error
// (internal/dhcp/link.go and friends). That is not duplication to remove: they
// declare the *facts they need* rather than importing ours, which is what keeps
// design.md §4.1's arrow pointing one way. The adapters in internal/daemon translate.
var ErrNoSuchInterface = errors.New("no such interface")

// Info is one interface as the rest of olr sees it.
//
// Deliberately the union of what dhcp, dns and routing each declare they need,
// and nothing more. Those three LinkInfo structs happen to be identical today;
// they are still three types, because the day one of them needs an MTU is the
// day the other two should not grow a field they do not use.
type Info struct {
	Name     string
	Adopted  bool
	Up       bool
	Prefixes []netip.Prefix

	// Present reports whether the kernel currently has this interface.
	//
	// False means it was adopted and is not there: a typo, a USB adapter that
	// is unplugged, a VLAN that has not been created yet. Distinguished from
	// "not up" because they need different answers from the operator, and
	// because flattening them would make `olr adopt eth9` look like it worked.
	Present bool
}

// Facts joins the kernel's interfaces with the adopted set.
type Facts struct {
	// Source reads the observed half. Nil means Kernel.
	Source Source

	// Store is the configuration document holding the adopted set.
	Store *core.Store
}

// source resolves the nil default in one place.
func (f Facts) source() Source {
	if f.Source == nil {
		return Kernel
	}
	return f.Source
}

// Config reads the stored half.
func (f Facts) Config() (Config, error) {
	if f.Store == nil {
		return Config{}, nil
	}
	doc, err := f.Store.Load()
	if err != nil {
		return Config{}, err
	}
	return FromDocument(doc)
}

// Interfaces lists everything link knows about, in a stable order.
//
// The union of two key sets, not just the kernel's: an interface is on the list
// if it exists *or* somebody adopted it. Dropping the second would make a typo
// in `olr adopt` invisible — the name would be stored, every module would keep
// refusing to use it, and the one screen that could explain why would show
// nothing at all.
func (f Facts) Interfaces() ([]Info, error) {
	observed, err := f.source()()
	if err != nil {
		return nil, err
	}
	cfg, err := f.Config()
	if err != nil {
		return nil, err
	}
	return Join(cfg, observed), nil
}

// Join is the join itself, as a function of its two inputs.
//
// Pure, and separate from Interfaces, so that a caller which has already read
// both halves — the HTTP list handler, which needs the same `observed` to
// validate against — does not read them a second time. Reading twice would not
// merely be wasteful: the two reads could disagree, and the list would then be
// validated against a machine state it is not describing.
func Join(cfg Config, observed []Interface) []Info {
	out := make([]Info, 0, len(observed)+len(cfg.Adopted))
	seen := make(map[string]bool, len(observed))
	for _, iface := range observed {
		seen[iface.Name] = true
		out = append(out, Info{
			Name:     iface.Name,
			Adopted:  cfg.IsAdopted(iface.Name),
			Up:       iface.Up,
			Prefixes: iface.Prefixes,
			Present:  true,
		})
	}
	for _, name := range cfg.Adopted {
		if !seen[name] {
			out = append(out, Info{Name: name, Adopted: true})
		}
	}

	// Absent interfaces sort to the end, after loopback: they are the least
	// useful rows and the ones most likely to be a mistake, so they read as a
	// footnote rather than as part of the machine's inventory.
	slices.SortStableFunc(out, func(a, b Info) int {
		if a.Present != b.Present {
			if a.Present {
				return -1
			}
			return 1
		}
		return 0
	})
	return out
}

// Interface returns what is known about one interface, or ErrNoSuchInterface.
func (f Facts) Interface(name string) (Info, error) {
	all, err := f.Interfaces()
	if err != nil {
		return Info{}, err
	}
	for _, info := range all {
		if info.Name == name {
			return info, nil
		}
	}
	return Info{}, fmt.Errorf("%q: %w", name, ErrNoSuchInterface)
}

// --- the development override ----------------------------------------------

// fileInterface is one entry of the `--links` file.
type fileInterface struct {
	Name     string   `json:"name,omitempty"`
	Up       bool     `json:"up"`
	Running  bool     `json:"running,omitempty"`
	Loopback bool     `json:"loopback,omitempty"`
	MAC      string   `json:"mac,omitempty"`
	Prefixes []string `json:"prefixes,omitempty"`

	// Adopted is accepted and ignored.
	//
	// The file used to carry adoption, because there was nowhere else to put it.
	// There is now — the configuration document — and having two sources for one
	// fact is what §4.1 forbids, so this field is parsed only so that an
	// existing file does not fail to load. Adopt through the UI or `olr adopt`.
	Adopted bool `json:"adopted,omitempty"`
}

// LoadFile reads interface facts from a JSON file keyed by interface name:
//
//	{"lan0": {"up": true, "prefixes": ["192.168.1.1/24"]}}
//
// This is the `--links` flag, and it is a development affordance rather than a
// deployment one: it lets olrd run against a plausible network on a machine that
// has none, which is what `make dev` needs. A packaged install reads the kernel.
func LoadFile(path string) (Source, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading interface facts: %w", err)
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var raw map[string]fileInterface
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	out := make([]Interface, 0, len(raw))
	for name, entry := range raw {
		if entry.Name != "" {
			name = entry.Name
		}
		iface := Interface{
			Name:         name,
			Up:           entry.Up,
			Running:      entry.Running || entry.Up,
			Loopback:     entry.Loopback,
			HardwareAddr: entry.MAC,
		}
		for _, p := range entry.Prefixes {
			prefix, err := netip.ParsePrefix(strings.TrimSpace(p))
			if err != nil {
				return nil, fmt.Errorf("parsing %s: %s: %w", path, name, err)
			}
			iface.Prefixes = append(iface.Prefixes, prefix)
		}
		out = append(out, iface)
	}
	sortInterfaces(out)

	// Read once and closed over. The file is a fixture, not a system to observe,
	// so re-reading it per request would only add a way for `make dev` to break
	// halfway through a session.
	return func() ([]Interface, error) { return out, nil }, nil
}
