package routing

import (
	"github.com/invopop/jsonschema"
)

// Schema descriptions for the module's own types.
//
// Core reflects the config struct into the schema that drives the REST body,
// the UI form, and the MCP tool definition (design.md §3.2 rule 3). Core can
// tell that these types marshal as strings — that is core.mapType's general
// rule — but only this package knows *which* strings are legal, so each
// vocabulary is declared here, next to the validator that enforces it.
//
// This is what makes each one a typed union in TypeScript rather than a bare
// string, which in turn is what makes the UI's exhaustive switches fail at
// compile time when a value is added instead of silently at runtime on the one
// screen nobody tests.
//
// Every method takes a value receiver, because that is what invopop detects.

// JSONSchema describes the probe duration format.
func (Duration) JSONSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Type:  "string",
		Title: "Duration",
		Description: "A duration with a unit, such as 30s, 5s or 1m30s. " +
			"Units are ns, us, ms, s, m and h.",
		Pattern:  `^([0-9]+(\.[0-9]+)?(ns|us|µs|ms|s|m|h))+$`,
		Examples: []any{"30s", "5s", "1m"},
	}
}

// JSONSchema publishes the exit forms as an enum.
//
// ViaKinds() exists precisely so this list has one source; spelling the values
// out again here would be the second copy that makes a UI drop-down and the
// validator disagree.
//
// Note what is *not* here. TPROXY (`local_socket`) is a real form in
// docs/gateway.md §1.2 and is v2 in §9, so it is absent rather than listed and
// refused: an enum value every generated surface offers and validation then
// rejects is a lie told by the contract three other surfaces are built from.
func (ViaKind) JSONSchema() *jsonschema.Schema {
	values := make([]any, 0, len(ViaKinds()))
	for _, k := range ViaKinds() {
		values = append(values, string(k))
	}

	return &jsonschema.Schema{
		Type:  "string",
		Title: "Exit form",
		Description: "How this exit delivers traffic. " +
			"interface sends it out a device — a WireGuard or Tailscale " +
			"interface, a PPPoE session, a proxy's TUN. " +
			"next_hop hands it to another box on the network, such as the " +
			"modem or a machine running a proxy. " +
			"blocked refuses it, so applications fail immediately and " +
			"visibly rather than hanging.",
		Enum: values,
	}
}

// JSONSchema publishes the IPv6 vocabulary as an enum.
func (IPv6Mode) JSONSchema() *jsonschema.Schema {
	// The empty string is legal and means block (see Exit.IPv6OrDefault), so it
	// belongs in the enum. Omitting it would make the schema reject a document
	// the module itself accepts.
	values := []any{""}
	for _, m := range IPv6Modes() {
		values = append(values, string(m))
	}

	return &jsonschema.Schema{
		Type:  "string",
		Title: "IPv6 handling",
		Description: "What happens to IPv6 traffic from sources assigned to " +
			"this exit. via carries it through the exit, which needs the exit " +
			"to actually have IPv6. block refuses it, so clients fall back to " +
			"IPv4 immediately. direct lets it take the box's normal path, " +
			"which leaks every site with an AAAA record around the exit. " +
			"Empty means block.",
		Enum:    values,
		Default: string(IPv6Block),
	}
}

// JSONSchema publishes the failure vocabulary as an enum.
func (FailureMode) JSONSchema() *jsonschema.Schema {
	values := []any{""}
	for _, f := range FailureModes() {
		values = append(values, string(f))
	}

	return &jsonschema.Schema{
		Type:  "string",
		Title: "Behaviour when the exit is down",
		Description: "What happens to assigned traffic when the health check " +
			"fails. block stops it, so the problem is visible and " +
			"diagnosable. direct sends it out the box's normal path instead, " +
			"which silently leaks exactly the traffic that was meant to be " +
			"routed. Empty means block.",
		Enum:    values,
		Default: string(FailBlock),
	}
}
