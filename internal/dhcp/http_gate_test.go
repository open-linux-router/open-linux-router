package dhcp

import (
	"bytes"
	"net/http"
	"os"
	"testing"
)

func TestDisruptiveHTTPChangeRequiresServerConfirmation(t *testing.T) {
	h, a, _ := testHTTP(t)
	if w := do(t, h, "PUT", "/config", validPUT); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	before, _ := os.ReadFile(a.Store.Path())
	rendered, _ := os.ReadFile(a.Paths.Conf)
	for _, query := range []string{"", "?dry_run=true", "?confirm=invalid"} {
		w := do(t, h, "PATCH", "/config"+query, `{"enabled":false}`)
		want := http.StatusConflict
		if query == "?dry_run=true" {
			want = 200
		}
		if query == "?confirm=invalid" {
			want = 400
		}
		if w.Code != want {
			t.Fatal(w.Code, w.Body)
		}
		after, _ := os.ReadFile(a.Store.Path())
		files, _ := os.ReadFile(a.Paths.Conf)
		if !bytes.Equal(before, after) || !bytes.Equal(rendered, files) {
			t.Fatal("held change wrote intent or runtime files")
		}
	}
	if w := do(t, h, "PATCH", "/config?confirm=true", `{"enabled":false}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	config, _ := a.Load()
	if config.Enabled {
		t.Fatal("confirmed change did not apply")
	}
}
