package packaging

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackageUpgradeRetiresOnlyOwnedProxyAndFailsIfStopFails(t *testing.T) {
	data, err := os.ReadFile("../../packaging/scripts/postinstall.sh")
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(string(data), "if systemctl is-active --quiet olr-shadowsocks.service;")
	end := strings.Index(string(data), "\nsystemctl daemon-reload")
	if start < 0 || end < start {
		t.Fatal("missing retirement before daemon reload")
	}
	for _, tc := range []struct {
		name, active, failStop string
		wantStop, wantFail     bool
	}{
		{"fresh", "no", "no", false, false},
		{"upgrade", "yes", "no", true, false},
		{"failed stop", "yes", "yes", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			stub := "#!/bin/sh\n[ \"$2\" = olr-shadowsocks.service ] || [ \"$3\" = olr-shadowsocks.service ] || exit 91\necho \"$1\" >> \"$RETIRE_LOG\"\ncase \"$1\" in\nis-active) [ \"$RETIRE_ACTIVE\" = yes ];;\nstop) [ \"$RETIRE_FAIL_STOP\" != yes ];;\n*) exit 0;;\nesac\n"
			if err := os.WriteFile(filepath.Join(dir, "systemctl"), []byte(stub), 0755); err != nil {
				t.Fatal(err)
			}
			log := filepath.Join(dir, "calls")
			cmd := exec.Command("sh", "-c", "set -e\n"+string(data)[start:end])
			cmd.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"), "RETIRE_LOG="+log, "RETIRE_ACTIVE="+tc.active, "RETIRE_FAIL_STOP="+tc.failStop)
			if err := cmd.Run(); (err != nil) != tc.wantFail {
				t.Fatalf("unexpected exit: %v", err)
			}
			calls, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(calls), "\nstop\n") != tc.wantStop {
				t.Fatalf("wrong stop behavior: %s", calls)
			}
			if !tc.wantFail && !strings.Contains(string(calls), "\nmask\n") {
				t.Fatalf("service can still start: %s", calls)
			}
		})
	}
}
