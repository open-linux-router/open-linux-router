package dns

import (
	"github.com/invopop/jsonschema"
)

// Schema descriptions for the module's own types.
//
// Core reflects the config struct into the schema that drives the REST body,
// the UI form, and the MCP tool definition (design.md §3.2 rule 3). Core can
// tell that these types marshal as strings — its general rule is that anything
// text-marshalling is a JSON string — but only this package knows *which*
// strings are legal. So the vocabulary is declared here, next to the parser
// that enforces it, rather than guessed at by the reflector.
//
// Both methods take value receivers because that is what invopop detects.

// JSONSchema publishes the upstream vocabulary as an enum.
//
// UpstreamModes() exists precisely so this list has one source; spelling the
// values out again here would be the second copy that makes a UI drop-down and
// the validator disagree.
func (UpstreamMode) JSONSchema() *jsonschema.Schema {
	// The empty string is a legal value meaning ModeRecurse (see
	// UpstreamMode.Valid), so it belongs in the enum. Omitting it would make
	// the schema reject a document the module itself accepts.
	values := []any{""}
	for _, m := range UpstreamModes() {
		values = append(values, string(m))
	}

	return &jsonschema.Schema{
		Type:  "string",
		Title: "Upstream mode",
		Description: "How names are resolved. recurse walks the DNS from the " +
			"root, so no third party sees everything this network looks up and " +
			"there is no forwarder to be down. forward sends every query to the " +
			"servers listed instead, which is faster from cold and the only " +
			"option where an upstream's own filtering is wanted. Empty means recurse.",
		Enum:    values,
		Default: string(ModeRecurse),
	}
}

// JSONSchema publishes the block-response vocabulary as an enum.
func (BlockResponse) JSONSchema() *jsonschema.Schema {
	values := []any{""}
	for _, r := range BlockResponses() {
		values = append(values, string(r))
	}

	return &jsonschema.Schema{
		Type:  "string",
		Title: "Blocked-name response",
		Description: "What a blocked name answers with. nxdomain says the name " +
			"does not exist, which is the honest answer and the one clients cache " +
			"and back off from. zero answers 0.0.0.0 and ::, which some networks " +
			"prefer because an app that reads NXDOMAIN as \"the network is down\" " +
			"will retry forever, where a refused connection fails at once. " +
			"Empty means nxdomain.",
		Enum:    values,
		Default: string(RespondNXDOMAIN),
	}
}
