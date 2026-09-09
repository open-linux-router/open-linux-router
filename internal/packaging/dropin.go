package packaging

import (
	"fmt"
	"strings"
)

// The paths the shipped units name, and the rendered configuration they point
// at. Debian and Ubuntu put these three binaries in /usr/sbin; Arch and Alpine
// use /usr/bin. The units name Debian's, because that is the packaged target,
// and anything else gets a drop-in rather than an edited unit — an edited unit
// is overwritten by the next upgrade, and a drop-in is not.
const (
	DebianDnsmasq  = "/usr/sbin/dnsmasq"
	DebianUnbound  = "/usr/sbin/unbound"
	DebianNft      = "/usr/sbin/nft"

	dhcpConf    = "/etc/open-linux-router/rendered/dhcp/dnsmasq.conf"
	dnsConf     = "/etc/open-linux-router/rendered/dns/unbound.conf"
	dnsAnchor   = "/var/lib/open-linux-router/dns/root.key"
	dnsHijack   = "/etc/open-linux-router/rendered/dns/hijack.nft"
	dropInName  = "10-path.conf"
	dropInDir   = "/etc/systemd/system"
	dropInBlurb = "# Written by `olr enable`: %s is not at the path this unit assumes.\n"
)

// Tools is where each backend was actually found on this box. An empty string
// means "not on PATH".
type Tools struct {
	Dnsmasq          string
	Unbound          string
	UnboundCheckconf string
	UnboundAnchor    string
	Nft              string
}

// DropIn is one systemd drop-in: a directory beside a unit, and one file in it.
type DropIn struct {
	Dir  string // e.g. /etc/systemd/system/olr-dhcp.service.d
	Path string // that directory plus the file name
	Data []byte
}

// DropIns returns the drop-ins this box needs, and nothing when every backend
// sits where the units already expect — which is the Debian and Ubuntu case,
// and so the common one.
//
// Pure, and separated from the writing for that reason: this is the part of
// installation with branches in it, and the shell script it replaces could not
// be tested at all without a root shell and a systemd.
func DropIns(t Tools) []DropIn {
	var out []DropIn
	if d := dhcpDropIn(t); d != nil {
		out = append(out, *d)
	}
	if d := dnsDropIn(t); d != nil {
		out = append(out, *d)
	}
	if d := relayDropIn(t); d != nil {
		out = append(out, *d)
	}
	return out
}

func dhcpDropIn(t Tools) *DropIn {
	if t.Dnsmasq == "" || t.Dnsmasq == DebianDnsmasq {
		return nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, dropInBlurb, "dnsmasq")
	b.WriteString("[Service]\n")
	// Cleared before being set: assigning to an Exec* directive appends to a
	// list, so without the empty assignment the unit would run both the
	// Debian path and this one.
	b.WriteString("ExecStartPre=\n")
	fmt.Fprintf(&b, "ExecStartPre=%s --test -C %s\n", t.Dnsmasq, dhcpConf)
	b.WriteString("ExecStart=\n")
	fmt.Fprintf(&b, "ExecStart=%s -k -C %s\n", t.Dnsmasq, dhcpConf)
	return dropIn("olr-dhcp.service", b.String())
}

func dnsDropIn(t Tools) *DropIn {
	if t.Unbound == "" || t.Unbound == DebianUnbound {
		return nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, dropInBlurb, "unbound")
	b.WriteString("[Service]\n")

	// Both ExecStartPre lines are cleared and rewritten together, because
	// clearing the directive resets the whole list rather than one entry —
	// reinstating only the checkconf would silently drop the trust-anchor
	// bootstrap, and a resolver that cannot validate is not a small loss.
	b.WriteString("ExecStartPre=\n")
	if t.UnboundAnchor != "" {
		// The leading `-` keeps a non-zero exit from failing the unit:
		// unbound-anchor exits non-zero when it *updated* the key, which is
		// the ordinary case.
		fmt.Fprintf(&b, "ExecStartPre=-%s -a %s\n", t.UnboundAnchor, dnsAnchor)
	}
	if t.UnboundCheckconf != "" {
		fmt.Fprintf(&b, "ExecStartPre=%s %s\n", t.UnboundCheckconf, dnsConf)
	}
	b.WriteString("ExecStart=\n")
	fmt.Fprintf(&b, "ExecStart=%s -d -p -c %s\n", t.Unbound, dnsConf)
	return dropIn("olr-dns.service", b.String())
}

func relayDropIn(t Tools) *DropIn {
	if t.Nft == "" || t.Nft == DebianNft {
		return nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, dropInBlurb, "nft")
	b.WriteString("[Service]\n")
	b.WriteString("ExecStartPost=\n")
	fmt.Fprintf(&b,
		"ExecStartPost=+-/bin/sh -c 'f=%s; test -f \"$f\" || exit 0; exec %s -f \"$f\"'\n",
		dnsHijack, t.Nft)
	b.WriteString("ExecStopPost=\n")
	fmt.Fprintf(&b, "ExecStopPost=+-%s delete table inet olr-dns\n", t.Nft)
	return dropIn("olr-dnsd.service", b.String())
}

func dropIn(unit, body string) *DropIn {
	dir := dropInDir + "/" + unit + ".d"
	return &DropIn{Dir: dir, Path: dir + "/" + dropInName, Data: []byte(body)}
}
