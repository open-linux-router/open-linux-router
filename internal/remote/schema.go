package remote

import (
	"github.com/invopop/jsonschema"
)

// Schema descriptions for the module's own types.
//
// Core reflects the config struct into the schema that drives the REST body,
// the UI form and the MCP tool definition (design.md §3.2 rule 3). Core can
// tell that these marshal as strings; only this package knows *which* strings
// are legal, so the vocabulary is declared next to the parser that enforces it.
//
// Value receivers, because that is what invopop detects.

// JSONSchema publishes the route vocabulary as an enum.
//
// This is the one field in the module an operator has to make a judgement
// about, so the description is the whole explanation rather than a label. A
// dropdown reading "home / everything" teaches nobody which one they want, and
// the consequence of picking the second one by accident — every byte the device
// sends crossing the operator's home upload — is not recoverable by looking at
// the tunnel.
func (RouteScope) JSONSchema() *jsonschema.Schema {
	// The empty string is legal and means RouteHome (RouteScope.Valid), so it
	// belongs in the enum. Omitting it would make the schema reject a document
	// the module itself accepts.
	values := []any{""}
	for _, s := range RouteScopes() {
		values = append(values, string(s))
	}

	return &jsonschema.Schema{
		Type:  "string",
		Title: "What the device sends through the tunnel",
		Description: "`home` sends only your home networks, so the device reaches " +
			"the things on them while everything else keeps going out of whatever " +
			"network the device is actually on. `everything` sends all of the " +
			"device's traffic here, so it appears to be at home for every purpose " +
			"including its public address — at the cost of every byte crossing your " +
			"home upload twice, and of needing address translation olr does not yet " +
			"write. Empty means home.",
		Enum:    values,
		Default: string(RouteHome),
	}
}

// JSONSchema publishes the cipher vocabulary as an enum, and carries the one
// piece of knowledge an operator needs in order to choose.
//
// The description does the work a list of names cannot: the two families are
// not "newer" and "older" in a way anybody can act on, and the thing that
// actually decides between them is whether every client can speak SIP022. The
// consequence of getting it wrong is a client that cannot connect, which looks
// like a network problem.
func (Cipher) JSONSchema() *jsonschema.Schema {
	// The empty string is legal and means DefaultCipher (Cipher.Valid), so it
	// belongs in the enum. Omitting it would make the schema reject a document
	// the module itself accepts.
	values := []any{""}
	for _, c := range Ciphers() {
		values = append(values, string(c))
	}

	return &jsonschema.Schema{
		Type:  "string",
		Title: "How proxy traffic is encrypted",
		Description: "The `2022-blake3-` ciphers are the current design and the " +
			"right choice unless a device is too old to speak it. Prefer the " +
			"chacha20 one on hardware with no AES instructions — an older ARM " +
			"board — where it is several times faster. The other two are the " +
			"previous generation, kept only for a client that cannot manage " +
			"the newer ones. Changing this regenerates the password, because a " +
			"2022 cipher's password is a fixed-length key rather than a " +
			"passphrase; every device then needs a new link. Empty means " +
			string(DefaultCipher) + ".",
		Enum:    values,
		Default: string(DefaultCipher),
	}
}
