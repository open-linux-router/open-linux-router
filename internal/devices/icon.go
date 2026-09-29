package devices

import (
	"fmt"
	"slices"
	"strings"

	"github.com/invopop/jsonschema"
)

// Icon is a picture an operator chose for a device, overriding the one its
// category and vendor would pick. Two shapes are legal:
//
//	<vendor>/<category>   a vendor's take on a kind of device: "apple/laptop"
//	os/<system>           an operating system's mark: "os/debian"
//
// The first is the answer to randomised MACs. A modern phone or laptop hides
// its maker from the registry, so the Apple pictures a detected vendor would
// select are unreachable for exactly the devices most likely to be Apple ones;
// letting the operator say so is the only way to get there. The second is for
// machines better known by what they run — a Proxmox host, a Debian VM — where
// a generic server picture says less than the logo does.
//
// Closed on both halves, like Category and VendorKey, so a stored icon always
// names something the UI can resolve or deliberately fall back from. Whether a
// picture has actually been *drawn* for a given vendor/category pair is the
// UI's business (see IMAGES in the SPA's features/devices/icons.ts): an
// undrawn pair falls back to the category's own picture rather than failing,
// which is what lets the artwork keep growing without a config migration.
type Icon string

// iconOSPrefix marks an operating-system icon.
const iconOSPrefix = "os/"

// operatingSystems is the vocabulary of OS marks, in the order a picker shows
// them: desktop and phone systems first, then the Linux family, then what a
// homelab runs. macOS and iOS share "apple" — the mark is the same, and their
// own wordmarks are unreadable at icon size.
var operatingSystems = []string{
	"windows", "apple", "android", "harmonyos",
	"linux", "ubuntu", "debian", "fedora", "archlinux",
	"freebsd", "proxmox", "openwrt", "raspberrypi",
}

// OperatingSystems returns the OS vocabulary in display order.
func OperatingSystems() []string {
	return slices.Clone(operatingSystems)
}

// Validate reports why i is not a legal icon, or nil. The empty icon is legal
// and means "follow category and vendor".
func (i Icon) Validate() error {
	if i == "" {
		return nil
	}
	s := string(i)
	if os, ok := strings.CutPrefix(s, iconOSPrefix); ok {
		if !slices.Contains(operatingSystems, os) {
			return fmt.Errorf("unknown operating system %q in icon %q; valid values are %s",
				os, s, strings.Join(operatingSystems, ", "))
		}
		return nil
	}

	vendor, category, ok := strings.Cut(s, "/")
	if !ok {
		return fmt.Errorf("icon %q is neither <vendor>/<category> nor os/<system>", s)
	}
	if !slices.Contains(VendorKeys(), VendorKey(vendor)) {
		return fmt.Errorf("unknown vendor %q in icon %q", vendor, s)
	}
	// A vendor's picture of "nothing in particular" is the one thing ICONS.md
	// refuses to draw, so the category half has to name a real kind of thing.
	if c := Category(category); c == "" || c == CategoryUnknown || !c.Valid() {
		return fmt.Errorf("icon %q needs a category after the vendor; valid values are %s",
			s, joinCategories())
	}
	return nil
}

// JSONSchema publishes the shape rather than the full list: vendor × category
// is several hundred strings, which would bloat the schema and every tool
// definition built from it for no gain over the validator's own message.
func (Icon) JSONSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Type:  "string",
		Title: "Device picture",
		Description: "A picture chosen for this device, overriding the one its " +
			"category and vendor would pick: <vendor>/<category> such as " +
			"\"apple/laptop\", or os/<system> such as \"os/debian\". Empty " +
			"means the picture follows category and vendor.",
		Pattern: `^$|^[a-z0-9-]+/[a-z0-9]+$`,
		Default: "",
	}
}
