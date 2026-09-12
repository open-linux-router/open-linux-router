package dial

import (
	"github.com/invopop/jsonschema"
)

// Schema descriptions for the module's own types.
//
// Core reflects the config struct into the schema that drives the REST body,
// the UI form and the MCP tool definition (design.md §3.2 rule 3). Core can tell
// that these types marshal as strings — that is core.mapType's general rule —
// but only this package knows *which* strings are legal, so each vocabulary is
// declared here, next to the validator that enforces it.
//
// Every method takes a value receiver, because that is what invopop detects.

// JSONSchema describes the check interval's format.
func (Duration) JSONSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Type:  "string",
		Title: "Duration",
		Description: "A duration with a unit, such as 5m, 30s or 1m30s. " +
			"Units are ns, us, ms, s, m and h.",
		Pattern:  `^([0-9]+(\.[0-9]+)?(ns|us|µs|ms|s|m|h))+$`,
		Examples: []any{"5m", "1m", "30s"},
	}
}

// JSONSchema publishes the providers as an enum.
//
// ProviderNames() exists precisely so this list has one source; spelling the
// values out again here would be the second copy that makes a UI drop-down and
// the validator disagree — and a name the drop-down offers and the validator
// refuses is exactly the fallthrough docs/ddns.md §4.4 is about, arriving from
// the other direction.
func (ProviderName) JSONSchema() *jsonschema.Schema {
	values := make([]any, 0, len(ProviderNames()))
	for _, n := range ProviderNames() {
		values = append(values, string(n))
	}

	return &jsonschema.Schema{
		Type:  "string",
		Title: "DNS provider",
		Description: "Which provider hosts the zone this name lives in. " +
			"cloudflare takes an API token. " +
			"alidns is Alibaba Cloud DNS and tencentcloud is Tencent Cloud DNSPod; " +
			"both take a key ID and a secret. " +
			"callback is the generic form: olr requests a URL you supply with the " +
			"address substituted into it, which is how the DynDNS-style endpoints " +
			"— No-IP, DuckDNS, Dynu — are reached. " +
			"The list grows by request rather than by completeness; a name outside " +
			"it is refused rather than guessed at.",
		Enum: values,
	}
}

// JSONSchema publishes the address sources as an enum.
//
// Note what is *not* here: an empty value. Every other enum in this tree admits
// one and gives it a meaning, because there is a safe default to fall back to.
// There is none here — one of these two answers makes this box talk to a third
// party every few minutes and the other does not — so the schema refuses the
// empty string exactly as the validator does (docs/ddns.md §3.1, design.md
// §5.6).
func (Source) JSONSchema() *jsonschema.Schema {
	values := make([]any, 0, len(Sources()))
	for _, s := range Sources() {
		values = append(values, string(s))
	}

	return &jsonschema.Schema{
		Type:  "string",
		Title: "Where the address comes from",
		Description: "interface reads the address off an uplink this router owns, " +
			"which is right when olr terminates the WAN — PPPoE, or DHCP from the ISP. " +
			"reflector asks an HTTPS endpoint what address the internet sees, which is " +
			"right when olr sits behind a modem, and is the only form that can tell you " +
			"your ISP has put you behind carrier-grade NAT. " +
			"There is no default: olr will not choose between talking to a third party " +
			"and not.",
		Enum: values,
	}
}
