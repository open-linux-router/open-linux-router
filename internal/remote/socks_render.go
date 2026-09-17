package remote

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Turning the SOCKS5 proxy's intent into the file 3proxy reads.
//
// Shaped like ss_render.go — a 0600 file holding a credential, plus a line a
// client can import — and it differs in one way worth stating up front: 3proxy's
// configuration is a line-oriented script rather than a document, so **what is
// left out matters as much as what is put in**. Three things are deliberately
// not rendered, and each has cost somebody an afternoon somewhere:
//
//  1. `daemon`. It forks. Under a `Type=simple` unit that hands systemd a
//     process that immediately exits, so the unit is reported as failed while
//     the proxy is in fact running — and `systemctl restart` then leaves two.
//     Running in the foreground is what a supervised daemon should do.
//  2. `nserver`. Pointing 3proxy at a specific resolver is documented upstream
//     as interacting badly with SOCKS5's UDP handling, and olr has no need for
//     it: without the directive 3proxy uses the system resolver, which on this
//     box is the one `dns` is already serving.
//  3. **A UDP flag.** See udpNotRendered below — this is the one place this
//     file admits to not knowing something.
//
// What is rendered is the smallest configuration that is certainly correct:
// require authentication, define one account, allow that account, listen.

// SocksUnitName is the systemd unit for the SOCKS5 proxy.
//
// `olr-socks5.service`, not `3proxy.service`: the distribution's own unit, if
// the operator's 3proxy came from a repository that ships one, points at
// /etc/3proxy/3proxy.cfg and is somebody else's. Same rule internal/ingress
// applies to Caddy and ss_render.go applies to shadowsocks-rust, and it is the
// difference between working and not on a box that has done this before.
const SocksUnitName = "olr-socks5.service"

// udpNotRendered records, in the one place a reader will look for it, why olr
// does not emit a UDP option for SOCKS5.
//
// SOCKS5 has a UDP ASSOCIATE command, 3proxy implements it, and DNS and QUIC
// both ride on it — so this is a real gap and not a shrug. What stopped it was
// that the flag could not be confirmed: the option most often quoted for this is
// `-u` on the `socks` line, and in 3proxy's own manual `-u` on a service line
// means **never ask the client for authentication**.
//
// Rendering it on a guess would therefore risk turning a credentialled proxy
// into an open one — silently, since the config would still look right and the
// proxy would still work, just for everybody. That is the worst class of bug
// this module could ship, and it is not worth a feature that an operator can
// add themselves in one line.
//
// So: not rendered, said out loud in the validator's warning, and reachable
// through raw_3proxy_conf for anyone who has checked their own 3proxy's manual.
// Resolve it by reading the manual of the packaged version and adding a test,
// not by trying it once on a box where authentication happened to be unused.
const udpNotRendered = "raw_3proxy_conf"

// RenderSocks turns intent into the file the proxy reads.
//
// It assumes the config has been validated; an unvalidated one produces
// nonsense rather than an error, which is why every caller validates first
// (design.md §5.3.1).
func RenderSocks(c Config, paths Paths) (Rendered, error) {
	conf, err := socksConf(c)
	if err != nil {
		return Rendered{}, err
	}
	return Rendered{Files: []File{{
		Path: paths.SocksConf,
		// 0600 and not a byte looser. The file holds the password in the clear —
		// 3proxy's `CL` credential type — and anyone who can read it can use the
		// proxy. systemd reads it as PID 1 before the service starts, so nothing
		// is lost by keeping it unreadable to everything else.
		Mode:   0o600,
		Data:   conf,
		Secret: true,
	}}}, nil
}

// socksListenAddr is the address 3proxy binds to, which is the whole point of
// the object.
//
// Tunnel scope resolves to this box's own address inside WireGuard, so the
// listening socket is only reachable by a peer that has already authenticated to
// the tunnel. Internet scope is spelled `0.0.0.0` explicitly rather than by
// omitting the flag: an omitted `-i` also means every interface, but it means it
// by default rather than by decision, and this is the one line of this file an
// operator should be able to read the intent off.
func socksListenAddr(c Config) (string, error) {
	if c.Socks.ListenScopeOrDefault() == ListenInternet {
		return "0.0.0.0", nil
	}
	addr := c.WireGuard.RouterAddr()
	if !addr.IsValid() {
		// Unreachable through the validator, which refuses this combination with
		// a message an operator can act on. Kept as an error rather than a
		// zero value because a bound-to-nothing proxy reports itself as running.
		return "", fmt.Errorf(
			"listen scope is %q but the tunnel has no address on this box", ListenTunnel)
	}
	return addr.String(), nil
}

func socksConf(c Config) ([]byte, error) {
	s := c.Socks
	addr, err := socksListenAddr(c)
	if err != nil {
		return nil, err
	}

	var b strings.Builder
	b.WriteString(socksHeader(c))
	b.WriteString("\n")

	// `auth strong` is what makes the users line mean anything. Without it
	// 3proxy accepts anonymous clients and the `allow` rule below is decoration
	// — which on an internet-scoped listener is an open proxy. There is no
	// configuration in which olr renders anything weaker.
	b.WriteString("auth strong\n")

	// CL is a cleartext credential, which is why the file is 0600. The
	// alternatives (NT/CR hashes) buy nothing here: the secret is generated by
	// olr, stored in olr's own config, and has to be handed to clients verbatim
	// anyway, so hashing it on disk would protect it from nobody who cannot
	// already read the config it came from.
	fmt.Fprintf(&b, "users %s:CL:%s\n", s.UserOrDefault(), s.Password)
	fmt.Fprintf(&b, "allow %s\n", s.UserOrDefault())

	fmt.Fprintf(&b, "socks -p%d -i%s\n", s.PortOrDefault(), addr)

	if extra := strings.TrimSpace(s.ExtraConf); extra != "" {
		// Appended last so it can add services or ACL lines after ours. 3proxy
		// reads its configuration top to bottom and applies the first matching
		// ACL, so an operator narrowing access has to come after `allow`, and
		// this is the position that makes that possible.
		b.WriteString("\n# raw_3proxy_conf\n")
		b.WriteString(extra)
		b.WriteString("\n")
	}

	return []byte(b.String()), nil
}

// socksHeader is the ownership banner every generated file carries
// (design.md §7).
func socksHeader(c Config) string {
	source := core.ConfigPath
	return fmt.Sprintf(`# 3proxy configuration — SOCKS5 for remote access
#
# Generated by open-linux-router from
#   %s
# Do not edit: this file is rewritten on every apply and your changes will be
# lost. Use `+"`olr remote set socks5`"+`, or the raw_3proxy_conf field for
# settings olr does not model.
`, source)
}

// SocksClientURL renders the `socks5://` line a client imports.
//
// The widely accepted form — `socks5://user:password@host:port` — which browsers,
// proxy switchers and most command-line tools accept directly. Credentials are
// percent-encoded, because a base64 password can contain `+` and `/` and both
// change meaning in a URL's userinfo.
//
// Like the Shadowsocks link and unlike a WireGuard peer's configuration, this
// can be produced again whenever it is asked for: there is one credential for
// every client and olr stores it, so there is nothing that exists for only an
// instant.
func SocksClientURL(c Config) (string, error) {
	s := c.Socks
	if strings.TrimSpace(s.Password) == "" {
		return "", fmt.Errorf("no password has been generated yet")
	}

	// Which host a client dials depends on where the proxy is listening, and
	// getting this wrong would hand somebody a line that cannot work. A
	// tunnel-scoped proxy is reached at this box's address *inside* the tunnel —
	// never at the public endpoint, which is where the tunnel itself is dialled.
	var host string
	if s.ListenScopeOrDefault() == ListenTunnel {
		addr := c.WireGuard.RouterAddr()
		if !addr.IsValid() {
			return "", fmt.Errorf("the tunnel has no address on this box yet")
		}
		host = fmt.Sprintf("%s:%d", addr, s.PortOrDefault())
	} else {
		host = c.DialAddress(s.DialPort())
		if host == "" {
			return "", fmt.Errorf("no endpoint is set, so there is no address for a client to dial")
		}
	}

	user := url.User(s.UserOrDefault())
	if s.Password != "" {
		user = url.UserPassword(s.UserOrDefault(), s.Password)
	}
	return "socks5://" + user.String() + "@" + host, nil
}
