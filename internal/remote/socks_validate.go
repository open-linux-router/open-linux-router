package remote

import (
	"strings"
)

// ValidateSocks checks the SOCKS5 proxy's half of a config.
func ValidateSocks(c Config) Result {
	var r Result
	s := c.Socks

	validateSocksRemovedScope(&r, s)
	validateSocksPorts(&r, c)
	validateSocksUser(&r, s)

	if s.Enabled && strings.TrimSpace(s.Password) == "" {
		// Reached only by a config edited by hand or restored with the secret
		// stripped: every path that enables the proxy through olr generates one
		// first.
		r.errorf("socks5.password",
			"this box has no password yet. `olr remote enable socks5` generates one")
	}

	// The proxy listens where anything that can route here can reach it, and
	// that is now the only shape it has — so the warning belongs to the object
	// rather than to a value the operator chose. It is attached at `socks5`
	// because there is no field to point at any more, and the honest reading of
	// the warning is "about this proxy", not "about one of its settings".
	if s.Enabled {
		r.warnf("socks5", "%s", SocksExposureWarning)
	}

	validateSocksExtra(&r, s)
	return r
}

// validateSocksRemovedScope refuses a document that still asks for the removed
// tunnel scope.
//
// Not converted, and deliberately not converted in silence: the proxy has one
// shape, and that shape is the one the internet can reach. An operator who
// selected `tunnel` asked for the opposite, so the only honest answers are "do
// what you asked" (impossible now) or "stop and let you decide" (this).
func validateSocksRemovedScope(r *Result, s Socks5) {
	if s.removedListen == "" {
		return
	}
	r.errorf("socks5",
		"`listen: %s` was removed: SOCKS5 always listens on every interface, and saying so "+
			"is the only shape it has. This proxy was confined to the tunnel, so applying it "+
			"now would put it on the internet — a decision olr will not make for you. If that "+
			"is what you want, save the proxy with `listen` set to `%s` (or drop the field); "+
			"if it is not, turn the proxy off first",
		s.removedListen, "internet")
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
