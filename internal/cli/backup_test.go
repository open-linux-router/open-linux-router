package cli

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIBackupAndRestorePreserveSecretsAndIntegers(t *testing.T) {
	body := `{"$schema":"test","firewall":{"counter":18446744073709551615},"secret":"secret-backup-value"}`
	var restored []string
	c := serveSocket(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/configuration/config" {
			_, _ = io.WriteString(w, body)
			return
		}
		if r.URL.Path == "/api/configuration/restore" {
			data, _ := io.ReadAll(r.Body)
			if string(data) != body {
				t.Errorf("CLI changed backup bytes: %s", data)
			}
			restored = append(restored, r.URL.RawQuery)
			_, _ = io.WriteString(w, `{"state":"idle"}`)
			return
		}
		_, _ = io.WriteString(w, `{"state":"idle"}`)
	}))
	path := filepath.Join(t.TempDir(), "backup.json")
	run := func(args ...string) (string, error) {
		root := NewRoot()
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&out)
		root.SetArgs(append([]string{"--socket", c.socket}, args...))
		err := root.Execute()
		return out.String(), err
	}
	out, err := run("backup", path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "secret-backup-value") || strings.Contains(out, "18446744073709551615") {
		t.Fatal("backup leaked content to stdout")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatal("backup credentials are not private")
	}
	if _, err := run("backup", path); err == nil {
		t.Fatal("backup overwrote existing file")
	}
	if _, err := run("restore", path); err == nil {
		t.Fatal("restore bypassed confirmation")
	}
	if _, err := run("restore", path, "--dry-run"); err != nil {
		t.Fatal(err)
	}
	if _, err := run("restore", path, "--confirm"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(restored, ",") != "dry_run=true,confirm=true" {
		t.Fatal("incorrect restore request", restored)
	}
}
