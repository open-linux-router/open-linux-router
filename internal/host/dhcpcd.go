package host

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// dhcpcd: the DHCP client Debian 13's ifupdown starts on an `inet dhcp`
// interface, and the one Raspberry Pi OS and Arch run as a service.
//
// It has no include directory, so olr's settings go into /etc/dhcpcd.conf
// itself, between markers, and giving them back is deleting what is between
// the markers. Two blocks, because dhcpcd.conf is positional — every option
// after an `interface` line belongs to that interface — so a global option
// appended at the end would silently apply to whatever interface the operator
// declared last:
//
//   - at the top, `nohook resolv.conf`, when the box's resolvers are olr's:
//     no interface's lease or router advertisement rewrites the file.
//   - at the end, `interface X` / `ipv6only` for each interface whose IPv4 is
//     olr's: dhcpcd stops asking for an IPv4 lease there, drops the IPv4 it
//     configured (on the box that forced this, a 169.254 fallback and a
//     default route through it), and keeps doing IPv6.
//
// A running dhcpcd is then told to re-read it with SIGHUP, its --rebind. See
// sysSignal: the signal that reads like "reconfigure" is its --release.

const (
	dhcpcdConf = "/etc/dhcpcd.conf"
	dhcpcdRun  = "/run/dhcpcd"

	blockBegin = "# BEGIN open-linux-router"
	blockEnd   = "# END open-linux-router"
)

// renderDhcpcd returns conf with olr's blocks replaced by the ones d calls
// for — or removed, when d calls for none.
func renderDhcpcd(conf string, d Desired) string {
	body := stripBlocks(conf)

	var b strings.Builder
	if len(d.Resolvers) > 0 {
		b.WriteString(blockBegin + ": this router's resolvers are olr's (the uplink); see `olr dial show uplink`\n")
		b.WriteString("nohook resolv.conf\n")
		b.WriteString(blockEnd + "\n\n")
	}
	b.WriteString(body)
	if ifaces := sortedUnique(d.IPv4); len(ifaces) > 0 {
		if body != "" && !strings.HasSuffix(body, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("\n" + blockBegin + ": IPv4 on these interfaces is olr's; dhcpcd keeps IPv6\n")
		for _, iface := range ifaces {
			fmt.Fprintf(&b, "interface %s\n\tipv6only\n", iface)
		}
		b.WriteString(blockEnd + "\n")
	}
	return b.String()
}

// stripBlocks removes every olr block, and the blank line olr put beside it,
// so rendering is idempotent and giving back leaves the file as it was.
//
// A begin marker with no end marker is left alone rather than taken to mean
// "to the end of the file": somebody editing the file by hand deleted the end
// marker, and everything after it is theirs.
func stripBlocks(conf string) string {
	lines := strings.SplitAfter(conf, "\n")
	var out []string
	for i := 0; i < len(lines); i++ {
		if !strings.HasPrefix(strings.TrimSpace(lines[i]), blockBegin) {
			out = append(out, lines[i])
			continue
		}
		end := i + 1
		for end < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[end]), blockEnd) {
			end++
		}
		if end == len(lines) {
			out = append(out, lines[i])
			continue
		}
		switch {
		case len(out) == 0 && end+1 < len(lines) && isBlank(lines[end+1]):
			end++ // the blank line olr wrote after a block at the top
		case allBlank(lines[end+1:]) && len(out) > 0 && isBlank(out[len(out)-1]):
			out = out[:len(out)-1] // the blank line olr wrote before a block at the end
		}
		i = end
	}
	return strings.Join(out, "")
}

func isBlank(line string) bool { return strings.TrimSpace(line) == "" }

func allBlank(lines []string) bool {
	for _, l := range lines {
		if !isBlank(l) {
			return false
		}
	}
	return true
}

// applyDhcpcd writes olr's blocks and tells every running dhcpcd to re-read.
//
// Nothing when dhcpcd is not installed: no config file, no client to stop.
func (a Applier) applyDhcpcd(d Desired) []core.Step {
	path := a.path(dhcpcdConf)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return []core.Step{{Description: "read " + dhcpcdConf, Error: err.Error()}}
	}

	next := renderDhcpcd(string(data), d)
	if next == string(data) {
		return nil
	}

	steps := []core.Step{step(describeDhcpcd(d), func() error {
		return core.WriteFileAtomic(path, []byte(next), 0o644)
	})}
	if steps[0].Error != "" {
		return steps
	}
	for _, pid := range a.dhcpcdPids() {
		steps = append(steps, step(fmt.Sprintf("tell dhcpcd (pid %d) to re-read %s", pid, dhcpcdConf),
			func() error { return a.signal(pid, SigReload) }))
	}
	return steps
}

func describeDhcpcd(d Desired) string {
	var parts []string
	if ifaces := sortedUnique(d.IPv4); len(ifaces) > 0 {
		parts = append(parts, "IPv4 on "+strings.Join(ifaces, ", ")+" is olr's")
	}
	if len(d.Resolvers) > 0 {
		parts = append(parts, "/etc/resolv.conf is olr's")
	}
	if len(parts) == 0 {
		return "hand " + dhcpcdConf + " back: remove olr's settings from it"
	}
	return "tell dhcpcd, in " + dhcpcdConf + ": " + strings.Join(parts, "; ")
}

// dhcpcdPids lists the dhcpcd processes to tell: the manager's pidfile, and
// one per interface for a dhcpcd started per interface, as ifupdown does.
//
// A pid is only returned while /proc says it is still dhcpcd. A pidfile
// outlives its process after a crash, and signalling whatever reused the
// number would be the worst thing this package could do.
func (a Applier) dhcpcdPids() []int {
	files, _ := filepath.Glob(filepath.Join(a.path(dhcpcdRun), "*.pid"))
	files = append(files, filepath.Join(a.path(dhcpcdRun), "pid"))

	var pids []int
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil || pid <= 1 {
			continue
		}
		comm, err := os.ReadFile(a.path(fmt.Sprintf("/proc/%d/comm", pid)))
		if err != nil || strings.TrimSpace(string(comm)) != "dhcpcd" {
			continue
		}
		pids = append(pids, pid)
	}
	return pids
}
