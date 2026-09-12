// Package packaging carries the files a working installation needs on disk,
// compiled into the binary.
//
// systemd reads unit files from the filesystem and there is no way around
// that: `systemctl start olrd` needs /lib/systemd/system/olrd.service to
// exist. So somebody has to write these four files. On the .deb path that is
// dpkg. On the standalone path it used to be a shell script shipped beside the
// binary, and is now `olr enable`, which is why they live in here — a single
// downloaded binary can put itself to work with nothing else in the directory.
//
// The .deb still installs the same files as real files rather than calling
// `olr enable` from its postinstall. dpkg has to *own* them, or `apt remove`
// leaves four units behind pointing at a binary that is gone.
//
// So there are two consumers and one copy: packaging/nfpm.yaml names these
// paths for the .deb, and the embed below reads them for `olr enable`.
// embed_test.go asserts the two lists are the same, because a unit added to
// one and forgotten in the other is exactly the drift that shows up as a
// missing service months later.
package packaging

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

//go:embed systemd/*.service olrd.env
var files embed.FS

// EnvPath is where olrd's unit reads its arguments from, and EnvName is the
// file's name inside this package.
const (
	EnvName = "olrd.env"
	EnvPath = "/etc/open-linux-router/olrd.env"
)

// UnitDir is where a distribution's own packages put unit files, and so where
// the .deb puts these. `olr enable` writes them to the same place: a unit in
// /etc/systemd/system would outrank one a later `apt install` laid down, which
// is how a box ends up running a stale unit nobody can find.
const UnitDir = "/lib/systemd/system"

// Unit is one systemd unit file: the name it is installed under, and its
// contents.
type Unit struct {
	Name string
	Data []byte
}

// Units returns every unit this installation needs, in a stable order.
//
// olrd.service is deliberately first. It is the only one `olr enable` enables,
// and the only one whose absence means olr does not work at all — the backends
// are enabled by their own modules when their configuration says the service
// is on (design.md §3.4), because enabling them here would put a DHCP server
// on the network and take over :53 on a box where nobody asked for either.
// olrPath is where this box's olr binary actually is. The shipped units name
// PackagedOLR, which is true for the .deb and false for every other way of
// installing, so the ExecStart lines naming it are rewritten on the way out.
func Units(olrPath string) ([]Unit, error) {
	entries, err := fs.ReadDir(files, "systemd")
	if err != nil {
		return nil, fmt.Errorf("reading embedded units: %w", err)
	}

	out := make([]Unit, 0, len(entries))
	for _, e := range entries {
		data, err := files.ReadFile("systemd/" + e.Name())
		if err != nil {
			return nil, fmt.Errorf("reading embedded unit %s: %w", e.Name(), err)
		}
		out = append(out, Unit{Name: e.Name(), Data: retargetOLR(data, olrPath)})
	}

	sort.Slice(out, func(i, j int) bool {
		if (out[i].Name == PrimaryUnit) != (out[j].Name == PrimaryUnit) {
			return out[i].Name == PrimaryUnit
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// PrimaryUnit is olr's own service — the control plane, and the one unit
// `olr enable` enables.
const PrimaryUnit = "olrd.service"

// PackagedOLR is the path the shipped unit files name, and where the .deb puts
// the binary. nfpm.yaml installs to exactly this path; embed_test.go asserts
// the units agree, so this constant cannot drift away from either.
const PackagedOLR = "/usr/bin/olr"

// retargetOLR rewrites the Exec lines that name the olr binary.
//
// The bug: two units — olrd.service and olr-dnsd.service — hardcode
// PackagedOLR, while `olr enable` installs the binary to /usr/local/bin,
// because /usr/bin belongs to dpkg and writing there corrupts the next
// `apt upgrade`. So every tarball install wrote units pointing at a file that
// was never created, systemd failed with 203/EXEC, and olrd sat in an
// auto-restart loop. The .deb path hid it completely: dpkg puts the binary
// exactly where the unit says, so the two only disagree on the install path
// that has no package manager to keep them in step.
//
// Rewritten into the unit rather than overridden by a drop-in, which is the
// opposite of what DropIns does for dnsmasq and unbound — and deliberately so.
// Those correct *another package's* unit, which apt owns and will overwrite, so
// the override has to survive outside it. This corrects our own unit, and a
// drop-in here would be actively harmful: it would pin /usr/local/bin/olr
// permanently, so a box that later installed the .deb would upgrade the binary
// at /usr/bin/olr while systemd kept executing the stale one beside it, with
// nothing on the box saying why the new version never took effect. Rendering
// the path in is self-healing instead — dpkg replaces the unit and the binary
// together, and a later `olr enable` re-renders whatever is true then.
func retargetOLR(unit []byte, olrPath string) []byte {
	if olrPath == "" || olrPath == PackagedOLR {
		return unit
	}

	lines := strings.Split(string(unit), "\n")
	for i, line := range lines {
		key, value, ok := strings.Cut(line, "=")
		// Only Exec* directives, and only when the binary is the first word:
		// a blind string replace would also rewrite the path where it appears
		// inside a comment, and comments here explain the packaged layout.
		if !ok || !strings.HasPrefix(key, "Exec") {
			continue
		}
		// systemd allows `+`, `-`, `!` and `:` prefixes on an Exec value.
		prefix := value[:len(value)-len(strings.TrimLeft(value, "+-!:@"))]
		rest := value[len(prefix):]
		if rest != PackagedOLR && !strings.HasPrefix(rest, PackagedOLR+" ") {
			continue
		}
		lines[i] = key + "=" + prefix + olrPath + rest[len(PackagedOLR):]
	}
	return []byte(strings.Join(lines, "\n"))
}

// Env returns the default contents of olrd.env.
//
// Written only when the file is absent, never over an existing one. It holds
// the one switch that matters — whether the web UI is reachable from the
// network — and an upgrade that silently closed a UI the operator opened would
// be the same surprise `type: config|noreplace` avoids on the .deb path.
func Env() ([]byte, error) {
	data, err := files.ReadFile(EnvName)
	if err != nil {
		return nil, fmt.Errorf("reading embedded %s: %w", EnvName, err)
	}
	return data, nil
}
