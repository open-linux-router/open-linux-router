package mcp

import "encoding/json"

// JSON-RPC 2.0, which is the envelope MCP speaks inside.
//
// Written out here rather than taken from a library, for the reason recorded in
// the package doc: the whole of what this server needs is one request shape, one
// response shape, and five error codes.

const jsonRPCVersion = "2.0"

// Standard JSON-RPC error codes. MCP adds no codes of its own for anything this
// server does — a tool that fails reports it *inside* a successful result (see
// toolResult), so none of these ever describe a tool's own failure.
const (
	codeParse          = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternal       = -32603
)

// request is one incoming call.
//
// ID is a json.RawMessage rather than a string or a number because JSON-RPC
// allows either and requires the response to echo it back *unchanged*. Decoding
// it into a Go type and re-encoding would turn a client's 1 into "1" for at
// least one popular client, and the mismatch shows up as a hung request rather
// than an error.
//
// Its absence is also the only thing that distinguishes a notification from a
// request, which is why it is a raw message and not, say, an int64 with a zero
// value that would be indistinguishable from an id of 0.
type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// isNotification reports whether no response may be sent.
func (r request) isNotification() bool { return len(r.ID) == 0 }

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func result(id json.RawMessage, v any) response {
	encoded, err := json.Marshal(v)
	if err != nil {
		// Marshalling our own result failed, which is a bug here rather than
		// anything the caller did. Report it as one instead of sending a
		// response with neither result nor error, which no client can read.
		return errorResponse(id, codeInternal, "encoding the result: "+err.Error())
	}
	return response{JSONRPC: jsonRPCVersion, ID: id, Result: encoded}
}

func errorResponse(id json.RawMessage, code int, message string) response {
	// An error to a request with no id still needs an id field, and JSON-RPC
	// says it is null in that case.
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	return response{
		JSONRPC: jsonRPCVersion,
		ID:      id,
		Error:   &rpcError{Code: code, Message: message},
	}
}
