package remote

import "github.com/open-linux-router/open-linux-router/internal/core"

// What distinguishes the two file-backed proxies from each other, which turns
// out to be seven facts and about 130 lines of nothing.
//
// ss_apply.go's ProxyApplier was written for one object and reads as general
// code with five Shadowsocks-shaped holes in it: which binary to look for, which
// unit to drive, where the file goes, which section says `enabled`, and which
// plan builder to call. Everything else — store intent first, make the
// directory, write the files, check the binary before the unit, move the unit,
// watch it settle — is identical for both, because both are "render a file and
// supervise a daemon".
//
// So this parameterises rather than copies. That is the same test the module
// applies everywhere else: an abstraction is justified when the two instances
// genuinely share a mechanism, and rejected when they only share a description.
// The tunnel does *not* appear here for exactly that reason — it writes no file
// and has no unit, so it keeps its own applier.

// ProxyObject selects which file-backed proxy an applier drives.
//
// The zero value is Shadowsocks, deliberately: it means every existing
// construction of ProxyApplier keeps its meaning without naming a field it
// predates.
type ProxyObject string

const (
	// ObjectShadowsocks is the zero value.
	ObjectShadowsocks ProxyObject = ""

	// ObjectSocks is the plain SOCKS5 proxy.
	ObjectSocks ProxyObject = "socks5"
)

// proxyKind is the per-object half of an applier's behaviour.
type proxyKind struct {
	// Object is how the object is named in step descriptions and blockers —
	// operator-facing, so "Shadowsocks" rather than a package identifier.
	Object string

	// Unit is the systemd unit this object's daemon runs under.
	Unit string

	// Conf is where the rendered file goes.
	Conf func(Paths) string

	// Enabled reads this object's on/off out of a whole config.
	Enabled func(Config) bool

	// Locate finds the backend binary, and Missing explains its absence.
	Locate  func() (string, error)
	Missing func() error

	// Build is the object's plan builder.
	Build func(desired, previous Config, paths Paths, obs ProxyObserved) (ProxyPlan, Rendered, error)
}

func (o ProxyObject) kind() proxyKind {
	if o == ObjectSocks {
		return proxyKind{
			Object:  "SOCKS5",
			Unit:    SocksUnitName,
			Conf:    func(p Paths) string { return p.SocksConf },
			Enabled: func(c Config) bool { return c.Socks.Enabled },
			Locate:  FindSocks,
			Missing: ErrSocksMissing,
			Build:   BuildSocksPlan,
		}
	}
	return proxyKind{
		Object:  "Shadowsocks",
		Unit:    UnitName,
		Conf:    func(p Paths) string { return p.Conf },
		Enabled: func(c Config) bool { return c.Shadowsocks.Enabled },
		Locate:  FindShadowsocks,
		Missing: ErrShadowsocksMissing,
		Build:   BuildProxyPlan,
	}
}

// SocksBlockers reports what stands between this box and a working SOCKS5
// proxy.
//
// Deliberately not a core.Dependency, for the reason socks_binary.go gives at
// length: 3proxy is not in Debian but publishes its own apt repository, so the
// fix is two steps and core/dependency.go can only render one — and would
// attach a button to it that fails until the repository is added.
func SocksBlockers() []core.Blocker {
	if _, err := FindSocks(); err == nil {
		return nil
	}
	return []core.Blocker{{
		Kind:    "missing_dependency",
		Summary: "No SOCKS5 server is installed on this box.",
		Detail: "olr does not implement SOCKS5 — 3proxy does, and olr renders its " +
			"configuration and supervises it.",
		Fix: socksInstallHint(),
	}}
}
