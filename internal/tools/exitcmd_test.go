//go:build linux

package tools

import (
	"errors"
	"os/exec"
	"reflect"
	"testing"
)

func withNFT(t *testing.T, run func(args ...string) error) *[][]string {
	t.Helper()
	var calls [][]string
	original := nft
	nft = func(args ...string) error {
		calls = append(calls, append([]string(nil), args...))
		return run(args...)
	}
	originalEUID := euid
	euid = func() int { return 0 }
	t.Cleanup(func() { nft = original; euid = originalEUID })
	return &calls
}

func TestMarkCommandLeavesTheDefaultRouteAlone(t *testing.T) {
	calls := withNFT(t, func(...string) error { return nil })
	cmd := exec.Command("true")
	cleanup, err := markCommand(cmd, 0)
	if err != nil {
		t.Fatal(err)
	}
	cleanup()
	if len(*calls) != 0 {
		t.Fatalf("default route touched nftables: %v", *calls)
	}
	if cmd.SysProcAttr != nil {
		t.Fatalf("default route changed the child credentials: %+v", cmd.SysProcAttr)
	}
}

func TestMarkCommandInstallsAndRemovesTheRule(t *testing.T) {
	calls := withNFT(t, func(...string) error { return nil })
	cmd := exec.Command("true")
	cleanup, err := markCommand(cmd, 0x00030000)
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"delete", "table", "inet", speedTable},
		{"add", "table", "inet", speedTable},
		{"add", "chain", "inet", speedTable, "output", "{ type route hook output priority mangle; policy accept; }"},
		{"add", "rule", "inet", speedTable, "output", "meta", "skgid", "2147418114", "meta", "mark", "set", "0x30000"},
	}
	if !reflect.DeepEqual(*calls, want) {
		t.Fatalf("nft calls = %v, want %v", *calls, want)
	}
	if cmd.SysProcAttr == nil || cmd.SysProcAttr.Credential == nil ||
		cmd.SysProcAttr.Credential.Gid != speedGID || !cmd.SysProcAttr.Credential.NoSetGroups {
		t.Fatalf("child credentials = %+v", cmd.SysProcAttr)
	}
	*calls = nil
	cleanup()
	if !reflect.DeepEqual(*calls, [][]string{{"delete", "table", "inet", speedTable}}) {
		t.Fatalf("cleanup calls = %v", *calls)
	}
}

func TestMarkCommandRemovesTheTableWhenTheRuleFails(t *testing.T) {
	failed := errors.New("nft failed")
	calls := withNFT(t, func(args ...string) error {
		if len(args) > 2 && args[2] == "rule" {
			return failed
		}
		return nil
	})
	cmd := exec.Command("true")
	if _, err := markCommand(cmd, 0x00030000); !errors.Is(err, failed) {
		t.Fatalf("error = %v, want %v", err, failed)
	}
	last := (*calls)[len(*calls)-1]
	if !reflect.DeepEqual(last, []string{"delete", "table", "inet", speedTable}) {
		t.Fatalf("failure left the table behind: %v", *calls)
	}
	if cmd.SysProcAttr != nil {
		t.Fatal("failed preparation still changed the child credentials")
	}
}

func TestMarkCommandNeedsRoot(t *testing.T) {
	originalNFT, originalEUID := nft, euid
	nft = func(...string) error {
		t.Fatal("nft must not run without root")
		return nil
	}
	euid = func() int { return 1000 }
	t.Cleanup(func() { nft, euid = originalNFT, originalEUID })
	if _, err := markCommand(exec.Command("true"), 0x00030000); err == nil {
		t.Fatal("marked a child while unprivileged")
	}
}
