package core

import (
	"encoding"
	"fmt"
	"net"
	"net/netip"
	"reflect"
	"strings"
	"time"

	"github.com/invopop/jsonschema"
)

// Schema reflection is the mechanism the whole surface story rests on
// (design.md §3.2 rule 3): a module's tagged config struct is the single source
// for the CLI flags, the REST body, the UI form, and the MCP tool definition.
// Add a field there and it appears everywhere, because nothing here is
// hand-written.
//
// Draft 2020-12, because OpenAPI 3.1 is a superset of it — so one dialect
// serves REST, MCP, and the UI without a translation layer (§8).

// Projections is a module's schema in the two forms design.md §10 requires.
//
// The two exist because reflection derives `required` from the *absence* of
// `omitempty`, not from a dedicated tag. That is the right rule for a full
// document, and exactly wrong for a partial update: without a relaxed
// projection, a single-field PATCH would fail validation against its own
// module's schema for omitting fields it was never trying to change.
type Projections struct {
	// Full validates a complete document: PUT, and the config file on disk.
	Full *jsonschema.Schema `json:"full"`

	// Relaxed validates a partial document: PATCH, and `olr set`. Identical to
	// Full except that every `required` list is empty.
	Relaxed *jsonschema.Schema `json:"relaxed"`
}

// reflector returns the one configured reflector, so that every module's schema
// is produced the same way.
func reflector() *jsonschema.Reflector {
	return &jsonschema.Reflector{
		// Inline the root object instead of emitting a bare $ref to it. Nested
		// types still land in $defs; this only spares every consumer one
		// indirection to reach the fields it actually wants.
		ExpandedStruct: true,

		// Deliberately left at the default (false): `required` comes from the
		// absence of `omitempty` on the struct tag, per §10. Turning this on
		// would introduce a second, contradictory source for requiredness —
		// which is the same objection that keeps a binding-tag framework out
		// of the HTTP layer.
		RequiredFromJSONSchemaTags: false,

		Mapper: mapType,
	}
}

// customSchema is the interface invopop checks for a type that describes
// itself. Mirrored here — with the same value-receiver-only test invopop uses —
// so mapType can decline those types instead of shadowing them.
var customSchema = reflect.TypeOf((*interface {
	JSONSchema() *jsonschema.Schema
})(nil)).Elem()

var textMarshaler = reflect.TypeOf((*encoding.TextMarshaler)(nil)).Elem()

// mapType supplies schemas for types whose Go shape does not match their JSON
// shape.
//
// This is not a nicety. netip.Addr is a struct with unexported fields that
// marshals as a *string*; reflected naively it publishes as
// `{"type":"object","properties":{}}`, and every surface derived from that —
// the UI form, the MCP tool, the OpenAPI document — is then wrong about the
// wire format. The same trap catches a duration wrapper: design.md §10 notes
// that a 12h lease must not serialise as 43200000000000, and a schema saying
// `integer` reintroduces exactly the lie the wrapper exists to prevent.
//
// Returning nil means "use the default reflection".
func mapType(t reflect.Type) *jsonschema.Schema {
	// A type that describes itself always wins. invopop consults Mapper before
	// it consults JSONSchema(), so without this the general rule below would
	// silently override a module's own, more precise, account of its type.
	if t.Implements(customSchema) {
		return nil
	}

	switch t {
	case reflect.TypeOf(netip.Addr{}):
		return &jsonschema.Schema{
			Type:        "string",
			Title:       "IP address",
			Description: "An IPv4 or IPv6 address, such as 192.168.1.1 or 2001:db8::1.",
		}
	case reflect.TypeOf(netip.Prefix{}):
		return &jsonschema.Schema{
			Type:        "string",
			Title:       "IP prefix",
			Description: "An address and prefix length in CIDR form, such as 192.168.1.0/24.",
		}
	case reflect.TypeOf(netip.AddrPort{}):
		return &jsonschema.Schema{
			Type:        "string",
			Title:       "Address and port",
			Description: "An address and port, such as 192.168.1.1:53 or [2001:db8::1]:53.",
		}
	case reflect.TypeOf(net.HardwareAddr{}):
		return &jsonschema.Schema{
			Type:        "string",
			Title:       "MAC address",
			Description: "A hardware address, such as aa:bb:cc:dd:ee:ff.",
		}
	case reflect.TypeOf(time.Time{}):
		// invopop already emits {"type":"string","format":"date-time"}, which
		// is better than what the general rule below would produce.
		return nil
	}

	// The general rule, and the reason the list above stays short: a type that
	// marshals as text *is* a JSON string, whatever its Go kind. Correct by
	// construction rather than by enumeration, so a module can add a
	// text-marshalling type without core having to learn about it.
	if t.Implements(textMarshaler) || reflect.PointerTo(t).Implements(textMarshaler) {
		return &jsonschema.Schema{Type: "string"}
	}

	return nil
}

// Reflect derives both projections from a module's config struct.
//
// v should be a zero value of the struct — only its type is read. name is the
// module's, and titles the root schema.
func Reflect(name string, v any) (Projections, error) {
	if v == nil {
		return Projections{}, fmt.Errorf("cannot reflect a nil schema value")
	}

	// Reflected twice rather than deep-copied. Relaxing mutates the tree in
	// place, and a copy that shared one sub-schema pointer with Full would
	// silently strip `required` from both.
	full := reflector().Reflect(v)
	relaxed := reflector().Reflect(v)
	relax(relaxed, map[*jsonschema.Schema]bool{})

	// Titled explicitly, because otherwise the only name on the root is the
	// Go import path in $id. Every consumer then invents its own: the type
	// generator would call this DhcpConfig by way of
	// HttpsGithubComOpenLinuxRouterOpenLinuxRouterInternalDhcpConfig, and an
	// MCP tool description would have nothing readable to show at all.
	title := configTitle(name)
	full.Title = title
	relaxed.Title = title

	return Projections{Full: full, Relaxed: relaxed}, nil
}

// configTitle turns a module name into the schema title, and so into the name
// of the generated TypeScript interface. Stable by construction: add a module
// and its type is named without anyone choosing.
func configTitle(name string) string {
	if name == "" {
		return "Config"
	}
	return strings.ToUpper(name[:1]) + name[1:] + "Config"
}

// relax clears `required` everywhere in the tree.
//
// It walks every keyword that can hold a sub-schema rather than just
// properties: a required field nested inside an array's items or one branch of
// a oneOf would otherwise survive and reject a legitimate partial update.
func relax(s *jsonschema.Schema, seen map[*jsonschema.Schema]bool) {
	if s == nil || seen[s] {
		return
	}
	seen[s] = true

	s.Required = nil
	s.DependentRequired = nil

	for _, def := range s.Definitions {
		relax(def, seen)
	}
	if s.Properties != nil {
		for pair := s.Properties.Oldest(); pair != nil; pair = pair.Next() {
			relax(pair.Value, seen)
		}
	}
	for _, m := range []map[string]*jsonschema.Schema{s.PatternProperties, s.DependentSchemas} {
		for _, sub := range m {
			relax(sub, seen)
		}
	}
	for _, list := range [][]*jsonschema.Schema{s.AllOf, s.AnyOf, s.OneOf, s.PrefixItems} {
		for _, sub := range list {
			relax(sub, seen)
		}
	}
	for _, sub := range []*jsonschema.Schema{
		s.Not, s.If, s.Then, s.Else,
		s.Items, s.Contains,
		s.AdditionalProperties, s.PropertyNames, s.ContentSchema,
	} {
		relax(sub, seen)
	}
}
