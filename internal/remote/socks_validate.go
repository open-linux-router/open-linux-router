package remote

import (
	"strings"
)

// The SOCKS5 proxy's rules. Pure — no files, no network, no root — so the whole
// set is table-tested and `olr remote` can check a config on a laptop.
//
// Two of these carry most of the object's weight, and neither exists for the
// object beside it:
//
//   - **the listen scope's coupling to the tunnel.** Defaulting to "inside
//     WireGuard" is only safe if that is checked, because a proxy told to bind an
//     address that does not exist starts, binds nothing, and reports itself as
//     running.
//   - **the escape hatch is an append, not a merge.** 3proxy's configuration is
//     a script read top to bottom, so a line in raw_3proxy_conf can undo the
//     authentication olr rendered above it. That is a different risk from the
//     Shadowsocks hatch, where a bad key produces a server that refuses to start
//     rather than one that quietly lets everybody in.

// ValidateSocks checks the SOCKS5 proxy's half of a config.
func ValidateSocks(c Config) Result {
	var r Result
	s := c.Socks

	if !s.Listen.Valid() {
		r.errorf("socks5.listen", "unknown listen scope %q (want %s or %s)",
			s.Listen, ListenTunnel, ListenInternet)
	}

	validateSocksScope(&r, c)
	validateSocksPorts(&r, c)
	validateSocksUser(&r, s)

	if s.Enabled && strings.TrimSpace(s.Password) == "" {
		// Reached only by a config edited by hand or restored with the secret
		// stripped: every path that enables the proxy through olr generates one
		// first.
		r.errorf("socks5.password",
			"this box has no password yet. `olr remote enable socks5` generates one")
	}

	validateSocksExtra(&r, s)
	return r
}

// validateSocksScope checks where the proxy will listen, which is this object's
// one real decision.
func validateSocksScope(r *Result, c Config) {
	s := c.Socks

	switch s.ListenScopeOrDefault() {
	case ListenTunnel:
		// The coupling, checked. Refused rather than warned because the failure
		// it prevents is invisible: 3proxy given an address no interface holds
		// starts cleanly, serves nobody, and looks healthy in `systemctl status`.
		if !c.WireGuard.Enabled {
			r.errorf("socks5.listen",
				"listening on the tunnel needs the tunnel: WireGuard is turned off, so there "+
					"is no address to bind to. Enable it, or set `listen` to %s and read the "+
					"warning that comes with it", ListenInternet)
			break
		}
		// Defensive, and not reachable today: the tunnel's subnet has a default,
		// so RouterAddr always resolves. Kept because the thing it guards
		// against fails silently — 3proxy given an address no interface holds
		// starts, binds nothing and looks healthy — and because that would
		// become reachable the moment the subnet stops having a default. A test
		// asserts the current behaviour so this comment cannot quietly go stale.
		if !c.WireGuard.RouterAddr().IsValid() {
			r.errorf("socks5.listen",
				"the tunnel has no address on this box yet, so there is nothing to bind to. "+
					"Set the tunnel's subnet first")
		}

	case ListenInternet:
		if s.Enabled {
			// A warning and never a refusal. design.md §5.6 puts the line at
			// whether olr can know the operator is wrong, and here it cannot: a
			// proxy reached only across a trusted private WAN, or through
			// somebody else's VPN, is a legitimate thing to run. So olr states
			// the three consequences and the one-setting remedy, and leaves the
			// decision where it belongs.
			r.warnf("socks5.listen", "%s", SocksExposureWarning)
		}
	}
}

func validateSocksPorts(r *Result, c Config) {
	s := c.Socks

	if s.ListenPort != 0 && s.ListenPort < 1024 {
		// Not refused: olr runs as root and the kernel will allow it. Worth a
		// remark because a low port is far more likely to be a typo than a
		// choice.
		r.warnf("socks5.listen_port",
			"%d is a privileged port; the convention is %d and nothing here needs a low one",
			s.ListenPort, DefaultSocksPort)
	}

	// Against Shadowsocks, because both take TCP and a collision is a unit that
	// will not start — reported one layer away from the field that caused it.
	// Not against WireGuard: that listens on UDP and this on TCP, so sharing a
	// number is legal and occasionally even deliberate.
	if s.PortOrDefault() == c.Shadowsocks.PortOrDefault() &&
		(s.Enabled || c.Shadowsocks.Enabled) {
		r.errorf("socks5.listen_port",
			"%d is already Shadowsocks' port, and both listen on TCP", s.PortOrDefault())
	}

	if s.PublicPort != 0 && s.ListenScopeOrDefault() == ListenTunnel {
		// Refused rather than ignored. A public port is a statement about a
		// router in front forwarding something, and nothing forwards a port into
		// a WireGuard tunnel — so a config carrying both is one where the
		// operator believes something that is not true.
		r.errorf("socks5.public_port",
			"a public port only means something when `listen` is %s: nothing forwards a port "+
				"into the tunnel", ListenInternet)
	}
}

func validateSocksUser(r *Result, s Socks5) {
	user := strings.TrimSpace(s.Username)
	if user == "" {
		return
	}
	// 3proxy's credential line is `users name:TYPE:secret`, split on colons and
	// whitespace. A name containing either would render a line that parses into
	// something else entirely — most likely an account nobody can authenticate
	// as, with a config that reads correctly.
	if strings.ContainsAny(user, ": \t\r\n") {
		r.errorf("socks5.username",
			"a username cannot contain spaces or colons: 3proxy's credential line is "+
				"`name:TYPE:secret` and this would split in the wrong place")
	}
}

// socksOwnedDirectives are the 3proxy directives this object renders itself,
// and what happens if the escape hatch repeats one.
//
// The hatch is appended after olr's lines, and 3proxy reads top to bottom — so
// these are not "duplicated", they are *overriding*, and two of them override
// the authentication. That is why this check refuses rather than warns.
var socksOwnedDirectives = map[string]string{
	"auth":  "olr renders `auth strong`, and a weaker setting here would replace it — an internet-scoped proxy with no authentication is an open proxy",
	"users": "the account is rendered from socks5.username and the generated password",
	"allow": "olr renders the rule that permits the account it created",
	"flush": "this clears the access rules olr rendered above it, which would leave the proxy with no `allow` line at all",
}

func validateSocksExtra(r *Result, s Socks5) {
	extra := strings.TrimSpace(s.ExtraConf)
	if extra == "" {
		return
	}

	for _, line := range strings.Split(extra, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		directive := strings.ToLower(line)
		if i := strings.IndexAny(directive, " \t"); i >= 0 {
			directive = directive[:i]
		}
		if why, owned := socksOwnedDirectives[directive]; owned {
			r.errorf("socks5.raw_3proxy_conf", "`%s` is rendered by this module: %s", directive, why)
			continue
		}
		if directive == "daemon" {
			// Rendered by nobody, and it must stay that way: forking under a
			// Type=simple unit hands systemd a process that exits immediately,
			// so the unit reports failed while the proxy runs.
			r.errorf("socks5.raw_3proxy_conf",
				"`daemon` forks, which breaks the systemd unit olr runs 3proxy under — "+
					"it would report failed while the proxy was in fact running")
		}
	}
}
