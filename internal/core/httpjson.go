package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// One JSON shape for every response and one for every error, decided here so
// that a client — the WebUI, the CLI, or an agent through MCP — never has to
// learn a second dialect per module.

// MaxBodyBytes caps a request body. A router's config is kilobytes; anything
// larger is a mistake or an attack, and either way we would rather say so than
// buffer it.
const MaxBodyBytes = 4 << 20 // 4 MiB

// Problem is one addressed complaint about a request, mirroring the shape
// modules already use for validation findings so a UI can attach it to the
// field that caused it.
type Problem struct {
	Path    string `json:"path,omitempty"`
	Message string `json:"message"`
}

// ErrorBody is the error envelope. Always an object under "error", never a
// bare string, so a client can branch on structure rather than on status code
// alone.
type ErrorBody struct {
	Message  string    `json:"message"`
	Problems []Problem `json:"problems,omitempty"`
}

// WriteJSON writes v with the given status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		// Marshalling our own response failed, so we cannot report it as JSON
		// either. Say so plainly rather than emitting a half-written body.
		http.Error(w, "internal error: encoding response", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// WriteError writes the error envelope.
func WriteError(w http.ResponseWriter, status int, message string, problems ...Problem) {
	WriteJSON(w, status, map[string]ErrorBody{
		"error": {Message: message, Problems: problems},
	})
}

// ReadBody reads a request body under the size cap.
func ReadBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBodyBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return nil, fmt.Errorf("request body exceeds %d bytes", MaxBodyBytes)
		}
		return nil, fmt.Errorf("reading request body: %w", err)
	}
	return data, nil
}

// DecodeJSON reads and strictly decodes a request body into dst.
//
// Unknown fields are an error. A typo in a field name silently doing nothing is
// the worst possible outcome for a config API: the operator sees a 200, assumes
// the setting took effect, and finds out from the network that it did not.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	data, err := ReadBody(w, r)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	if dec.More() {
		return errors.New("invalid JSON body: trailing data after the top-level value")
	}
	return nil
}

// MergePatch applies an RFC 7386 JSON merge patch to orig and returns the
// result.
//
// This is what PATCH and `olr set` are: change one field without restating the
// whole document. design.md §10 requires a relaxed schema projection for
// exactly this path, because a single-field update would otherwise fail
// validation against its own schema's `required` list.
//
// Merge-patch semantics, which are the surprising part and are inherited
// deliberately rather than invented here:
//
//   - null removes a key
//   - objects merge recursively
//   - **arrays are replaced wholesale**, never merged element-wise
//
// The last one is why adding a single reservation is `POST`-shaped work in the
// module, not a PATCH of the reservations array.
func MergePatch(orig, patch []byte) ([]byte, error) {
	var target any
	if len(orig) > 0 {
		if err := json.Unmarshal(orig, &target); err != nil {
			return nil, fmt.Errorf("decoding current document: %w", err)
		}
	}
	var p any
	if err := json.Unmarshal(patch, &p); err != nil {
		return nil, fmt.Errorf("decoding patch: %w", err)
	}
	return json.Marshal(mergeValue(target, p))
}

func mergeValue(target, patch any) any {
	patchObj, ok := patch.(map[string]any)
	if !ok {
		// A non-object patch replaces the target outright.
		return patch
	}
	targetObj, ok := target.(map[string]any)
	if !ok {
		targetObj = map[string]any{}
	}
	for k, v := range patchObj {
		if v == nil {
			delete(targetObj, k)
			continue
		}
		targetObj[k] = mergeValue(targetObj[k], v)
	}
	return targetObj
}
