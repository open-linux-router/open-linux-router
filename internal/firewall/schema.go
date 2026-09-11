package firewall

import (
	"github.com/invopop/jsonschema"
)

// Schema descriptions for the module's own types.
//
// Core reflects the config struct into the schema that drives the REST body,
// the UI form and the MCP tool definition (design.md §3.2 rule 3). Core can tell
// that these types marshal as strings — that is core.mapType's general rule for
// anything implementing encoding.TextMarshaler — but only this package knows
// *which* strings are legal, so each vocabulary is declared here, next to the
// validator that enforces it.
//
// This is the trap design.md §3.2 rule 3 exists to catch, and it is quiet: a
// type that marshals as a string and does not say so publishes as a bare
// `string` on every generated surface at once. The UI gets a free-text box
// where a drop-down belongs, the MCP tool description tells an agent nothing
// about what it may pass, and TypeScript's exhaustive switches stop failing at
// compile time when a value is added — they start failing at runtime, on the
// one screen nobody tests.
//
// Every method takes a value receiver, because that is what invopop detects.

// JSONSchema publishes the transport vocabulary as an enum.
//
// Protocols() exists precisely so this list has one source; spelling the values
// out again here would be the second copy that makes a UI drop-down and the
// validator disagree.
func (Protocol) JSONSchema() *jsonschema.Schema {
	// The empty string is legal and means tcp (see Forward.ProtocolOrDefault),
	// so it belongs in the enum. Omitting it would make the schema reject a
	// document the module itself accepts.
	values := []any{""}
	for _, p := range Protocols() {
		values = append(values, string(p))
	}

	return &jsonschema.Schema{
		Type:  "string",
		Title: "Protocol",
		Description: "Which kind of traffic this forward carries. " +
			"tcp covers web, SSH and most services. " +
			"udp covers games, voice and VPNs. " +
			"both carries each, as two rules sharing one counter. " +
			"Empty means tcp.",
		Enum:    values,
		Default: string(ProtocolTCP),
	}
}

// JSONSchema describes the port format.
//
// A pattern rather than a bare string, because this is the field an agent or a
// form is most likely to guess at: `8080/tcp` and `8080:80` are both plausible
// spellings of something, and neither is this one. The pattern rejects them at
// the surface instead of letting UnmarshalText explain it one request later.
func (PortRange) JSONSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Type:  "string",
		Title: "Port or port range",
		Description: "A port, such as 8080, or an inclusive range, such as " +
			"30000-30010. A range is forwarded to the identical range inside; " +
			"only a single port may be remapped to a different one.",
		Pattern:  `^[1-9][0-9]{0,4}(-[1-9][0-9]{0,4})?$`,
		Examples: []any{"8080", "443", "30000-30010"},
	}
}
