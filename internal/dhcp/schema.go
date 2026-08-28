package dhcp

import (
	"github.com/invopop/jsonschema"
)

// Schema descriptions for the module's own types.
//
// Core reflects the config struct into the schema that drives the REST body,
// the UI form, and the MCP tool definition (design.md §3.2 rule 3). Core can
// tell that these types marshal as strings, but only this package knows *which*
// strings are legal — so the vocabulary is declared here, next to the parser
// that enforces it, rather than guessed at by the reflector.
//
// Both methods take value receivers because that is what invopop detects.

// JSONSchema describes the lease-time format.
//
// Without this the field publishes as a bare string and every surface has to
// discover by trial and error that "2d" is accepted and "2 days" is not.
func (Duration) JSONSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Type:  "string",
		Title: "Duration",
		Description: "A duration written the way an operator would say it: " +
			"a number and a unit, optionally repeated. Units are s, m, h, d and w. " +
			"A bare number means seconds, matching dnsmasq.",
		Pattern:  `^([0-9]+[smhdw]?)+$`,
		Examples: []any{"45m", "12h", "2d", "1w"},
	}
}

// JSONSchema publishes the RA vocabulary as an enum.
//
// RAModes() exists precisely so this list has one source; spelling the values
// out again here would be the second copy that makes a UI drop-down and the
// validator disagree.
func (RAMode) JSONSchema() *jsonschema.Schema {
	// The empty string is a legal value meaning RAOff (see RAMode.Valid), so it
	// belongs in the enum. Omitting it would make the schema reject a document
	// the module itself accepts.
	values := []any{""}
	for _, m := range RAModes() {
		values = append(values, string(m))
	}

	return &jsonschema.Schema{
		Type:  "string",
		Title: "Router advertisement mode",
		Description: "How IPv6 is served on this pool's interface. " +
			"off serves no IPv6; slaac advertises the prefix and answers " +
			"DHCPv6 information requests; stateful additionally hands out " +
			"addresses over DHCPv6. Empty means off.",
		Enum:    values,
		Default: string(RAOff),
	}
}
