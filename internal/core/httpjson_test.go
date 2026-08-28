package core

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMergePatch(t *testing.T) {
	tests := []struct {
		name  string
		orig  string
		patch string
		want  string
	}{
		{
			name:  "sets one field and leaves the rest",
			orig:  `{"enabled":false,"name":"lan"}`,
			patch: `{"enabled":true}`,
			want:  `{"enabled":true,"name":"lan"}`,
		},
		{
			name:  "null removes a key",
			orig:  `{"enabled":true,"domain":"lan"}`,
			patch: `{"domain":null}`,
			want:  `{"enabled":true}`,
		},
		{
			// The surprising rule, inherited from RFC 7386 deliberately rather
			// than invented: this is why adding one reservation is a PUT of the
			// new document and not a PATCH of the array.
			name:  "arrays are replaced wholesale, never merged",
			orig:  `{"pools":[{"interface":"lan0"},{"interface":"lan1"}]}`,
			patch: `{"pools":[{"interface":"lan2"}]}`,
			want:  `{"pools":[{"interface":"lan2"}]}`,
		},
		{
			name:  "objects merge recursively",
			orig:  `{"a":{"b":1,"c":2}}`,
			patch: `{"a":{"c":3}}`,
			want:  `{"a":{"b":1,"c":3}}`,
		},
		{
			name:  "an absent original is built from the patch",
			orig:  ``,
			patch: `{"enabled":true}`,
			want:  `{"enabled":true}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := MergePatch([]byte(tt.orig), []byte(tt.patch))
			if err != nil {
				t.Fatalf("MergePatch: %v", err)
			}
			if !sameJSON(t, got, []byte(tt.want)) {
				t.Errorf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestMergePatchRejectsMalformedInput(t *testing.T) {
	if _, err := MergePatch([]byte(`{}`), []byte(`{ nope`)); err == nil {
		t.Error("want an error for a malformed patch")
	}
	if _, err := MergePatch([]byte(`{ nope`), []byte(`{}`)); err == nil {
		t.Error("want an error for a malformed original")
	}
}

// A mistyped field silently doing nothing is the worst outcome for a config
// API: the operator sees a 200 and finds out from the network that it did not
// take.
func TestDecodeJSONRejectsUnknownFields(t *testing.T) {
	var dst struct {
		Enabled bool `json:"enabled"`
	}
	r := httptest.NewRequest(http.MethodPut, "/config", strings.NewReader(`{"enabledd":true}`))
	err := DecodeJSON(httptest.NewRecorder(), r, &dst)
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Errorf("err = %v, want an unknown-field error", err)
	}
}

func TestDecodeJSONRejectsTrailingData(t *testing.T) {
	var dst map[string]any
	r := httptest.NewRequest(http.MethodPut, "/config", strings.NewReader(`{} {}`))
	if err := DecodeJSON(httptest.NewRecorder(), r, &dst); err == nil {
		t.Error("want an error for trailing data")
	}
}

func TestWriteErrorShape(t *testing.T) {
	w := httptest.NewRecorder()
	WriteError(w, http.StatusUnprocessableEntity, "invalid",
		Problem{Path: "pools[0].start", Message: "outside the subnet"})

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content-type = %q", ct)
	}

	var body struct {
		Error ErrorBody `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshalling body: %v", err)
	}
	if body.Error.Message != "invalid" {
		t.Errorf("message = %q", body.Error.Message)
	}
	if len(body.Error.Problems) != 1 || body.Error.Problems[0].Path != "pools[0].start" {
		t.Errorf("problems = %+v", body.Error.Problems)
	}
}

func sameJSON(t *testing.T, a, b []byte) bool {
	t.Helper()
	var x, y any
	if err := json.Unmarshal(a, &x); err != nil {
		t.Fatalf("unmarshalling %s: %v", a, err)
	}
	if err := json.Unmarshal(b, &y); err != nil {
		t.Fatalf("unmarshalling %s: %v", b, err)
	}
	ax, _ := json.Marshal(x)
	by, _ := json.Marshal(y)
	return string(ax) == string(by)
}
