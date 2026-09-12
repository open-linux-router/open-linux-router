package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The bug: dnsmasq, unbound and nft live in /usr/sbin on Debian, and /usr/sbin
// is not on an ordinary user's PATH there. `olr enable` looked only at $PATH,
// so it reported "dnsmasq is not on PATH" on a box where dnsmasq was installed
// and running, and told the operator to install it again — into the full
// dnsmasq package, which is the one olr deliberately does not want.
func TestLookToolFindsABackendInSbin(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dnsmasq-for-test")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	restore := sbinDirs
	sbinDirs = []string{dir}
	t.Cleanup(func() { sbinDirs = restore })

	if got := lookTool("dnsmasq-for-test"); got != path {
		t.Errorf("lookTool() = %q; want %q — a daemon off $PATH must still be found", got, path)
	}
}

// A file that is there but not executable is not a backend. Returning it would
// put an ExecStart in a unit that systemd then fails on, which is a worse
// failure than saying the tool is missing.
func TestLookToolIgnoresWhatItCannotRun(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "unbound-for-test"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "nft-for-test"), 0o755); err != nil {
		t.Fatal(err)
	}

	restore := sbinDirs
	sbinDirs = []string{dir}
	t.Cleanup(func() { sbinDirs = restore })

	if got := lookTool("unbound-for-test"); got != "" {
		t.Errorf("lookTool() = %q for a non-executable file; want \"\"", got)
	}
	if got := lookTool("nft-for-test"); got != "" {
		t.Errorf("lookTool() = %q for a directory; want \"\"", got)
	}
}

func TestLookToolReportsNothingWhenThereIsNothing(t *testing.T) {
	restore := sbinDirs
	sbinDirs = []string{t.TempDir()}
	t.Cleanup(func() { sbinDirs = restore })

	if got := lookTool("olr-no-such-backend"); got != "" {
		t.Errorf("lookTool() = %q; want \"\"", got)
	}
}

// The message has to name dnsmasq-base and say why, because `apt install
// dnsmasq` is what everybody types and it installs a service that binds :53.
// It also has to say where it looked: "not on PATH" is a claim about the
// operator's shell that they cannot check against a PATH we never showed them.
func TestMissingToolExplainsWhichPackageAndWhereItLooked(t *testing.T) {
	msg := missingTool("dnsmasq", "dnsmasq-base", "olr does not implement DHCP itself.",
		"dnsmasq-base rather than dnsmasq: the full package also ships a service.").Error()

	for _, want := range []string{
		"dnsmasq was not found",
		"apt install dnsmasq-base",
		"/usr/sbin",
		"$PATH",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("missing %q:\n%s", want, msg)
		}
	}
}

// Every unit olr warns about must carry advice, and systemd-resolved's must not
// be "disable it" — the box resolves through it.
func TestDistroBackendsCarryAdvice(t *testing.T) {
	for _, b := range distroBackends {
		if b.advice == "" {
			t.Errorf("%s has no advice", b.unit)
		}
		if b.unit == "systemd-resolved.service" {
			if !strings.Contains(b.advice, "DNSStubListener=no") {
				t.Errorf("systemd-resolved advice should hand over the socket, not stop it:\n%s", b.advice)
			}
			if strings.Contains(b.advice, "disable --now systemd-resolved") {
				t.Errorf("told the operator to disable the resolver the box depends on:\n%s", b.advice)
			}
			continue
		}
		if !strings.Contains(b.advice, "disable --now "+b.unit) {
			t.Errorf("%s advice does not disable it:\n%s", b.unit, b.advice)
		}
	}
}
