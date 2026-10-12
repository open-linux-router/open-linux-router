package link

import (
	"bytes"
	"net/http"
	"os"
	"testing"
)

func TestRenumberingIsHeldBeforeTheKernelOrIntentChanges(t *testing.T) {
	h, a := testHTTP(t, `{"link":{"adopted":["lan0"],"networks":[{"name":"lan","members":["lan0"],"ipv4":{"subnet":"192.168.1.0/24"}}]}}`)
	before, _ := os.ReadFile(a.Store.Path())
	body := `{"networks":[{"name":"lan","members":["lan0"],"ipv4":{"subnet":"172.16.1.0/24"}}]}`
	for _, query := range []string{"", "?dry_run=true"} {
		w := do(t, h, "PATCH", "/config"+query, body)
		want := http.StatusConflict
		if query != "" {
			want = http.StatusOK
		}
		if w.Code != want {
			t.Fatal(w.Code, w.Body)
		}
		after, _ := os.ReadFile(a.Store.Path())
		if !bytes.Equal(before, after) {
			t.Fatal("held change stored intent")
		}
		// No fake writer is installed: calling the kernel would fail the request.
	}
}
