package ingress

import (
	"net/netip"
	"strings"
	"testing"
)

// good returns a config that validates cleanly, so each test can break exactly
// one thing and the failure it asserts is unambiguous.
func good() Config {
	return Config{
		Enabled: true,
		Certificate: Certificate{
			Provider: "cloudflare",
			Token:    "tok",
			Email:    "ops@example.com",
		},
		Services: []Service{{
			Name:     "grafana",
			Upstream: Upstream{Device: "nuc", Port: 3000},
		}},
	}
}

func goodDNS() StaticDNS {
	return StaticDNS{Domain: "home.example.com", HostNames: []string{"printer"}}
}

func goodDevices() StaticDevices {
	return StaticDevices{
		"nuc": {Addr: netip.MustParseAddr("192.168.1.50"), Fixed: true},
		"tv":  {Addr: netip.MustParseAddr("192.168.1.77")},
	}
}

// findError reports whether any error is attached to path and mentions want.
func findError(t *testing.T, r Result, path, want string) {
	t.Helper()
	for _, p := range r.Errors {
		if p.Path == path && strings.Contains(p.Message, want) {
			return
		}
	}
	t.Fatalf("no error at %q containing %q; got %v", path, want, r.Errors)
}

func TestValidateAcceptsAGoodConfig(t *testing.T) {
	r := Validate(good(), goodDNS(), goodDevices())
	if !r.OK() {
		t.Fatalf("expected valid, got %v", r.Errors)
	}
	if len(r.Warnings) != 0 {
		t.Fatalf("expected no warnings, got %v", r.Warnings)
	}
}

// The first error most operators will ever see from this module: `dns`'s own
// default local domain cannot carry a public certificate.
func TestValidateRefusesTheDefaultLocalDomain(t *testing.T) {
	r := Validate(good(), StaticDNS{Domain: "home.arpa"}, goodDevices())
	findError(t, r, "", "no certificate authority will issue")
	if !strings.Contains(r.Errors[0].Message, "olr dns set --local-domain") {
		t.Fatalf("the refusal must carry the remedy, got %q", r.Errors[0].Message)
	}
}

func TestValidateRefusesUnissuableDomains(t *testing.T) {
	for _, domain := range []string{"home.arpa", "my.lan", "box.local", "x.internal", "a.test"} {
		t.Run(domain, func(t *testing.T) {
			r := Validate(good(), StaticDNS{Domain: domain}, goodDevices())
			findError(t, r, "", "reserved")
		})
	}
}

func TestValidateRefusesASingleLabelDomain(t *testing.T) {
	r := Validate(good(), StaticDNS{Domain: "router"}, goodDevices())
	findError(t, r, "", "single label")
}

func TestValidateSkipsDomainChecksWhenDisabled(t *testing.T) {
	c := good()
	c.Enabled = false
	// A disabled module must not hold the config invalid — the operator has to
	// be able to save an incomplete setup and come back to it.
	if r := Validate(c, StaticDNS{Domain: "home.arpa"}, goodDevices()); !r.OK() {
		t.Fatalf("disabled config should validate, got %v", r.Errors)
	}
}

func TestValidateCertificate(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Config)
		path string
		want string
	}{
		{"no provider", func(c *Config) { c.Certificate.Provider = "" }, "certificate.provider", "required"},
		{"no token", func(c *Config) { c.Certificate.Token = "" }, "certificate.provider_token", "required"},
		{"named resolver", func(c *Config) { c.Certificate.Resolvers = []string{"dns.example.com"} },
			"certificate.resolvers[0]", "not an IP address"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := good()
			tc.edit(&c)
			findError(t, Validate(c, goodDNS(), goodDevices()), tc.path, tc.want)
		})
	}
}

// The provider name is deliberately not checked here. Whether the binary has
// that module is a question about a file on disk, and this layer is pure — the
// real check is `caddy validate` on the rendered file, before it is applied.
func TestValidateDoesNotJudgeTheProviderName(t *testing.T) {
	c := good()
	c.Certificate.Provider = "something-only-their-build-has"
	if r := Validate(c, goodDNS(), goodDevices()); !r.OK() {
		t.Fatalf("expected valid, got %v", r.Errors)
	}
}

func TestValidateAcceptsResolversWithPorts(t *testing.T) {
	c := good()
	c.Certificate.Resolvers = []string{"1.1.1.1", "9.9.9.9:53", "[2606:4700:4700::1111]:53"}
	if r := Validate(c, goodDNS(), goodDevices()); !r.OK() {
		t.Fatalf("expected valid, got %v", r.Errors)
	}
}

// A nested name renders into a perfectly valid Caddyfile and then fails TLS at
// the first request, because a wildcard covers exactly one label.
func TestValidateRefusesANestedName(t *testing.T) {
	c := good()
	c.Services[0].Name = "grafana.lab"
	r := Validate(c, goodDNS(), goodDevices())
	findError(t, r, "services[0].name", "more than one label")
}

// The silent one: `dns` answers first and the proxy is never reached.
func TestValidateRefusesANameDNSAlreadyAnswers(t *testing.T) {
	c := good()
	c.Services[0].Name = "printer"
	r := Validate(c, goodDNS(), goodDevices())
	findError(t, r, "services[0].name", "would never reach the proxy")
}

func TestValidateNameCollisionIgnoresSpelling(t *testing.T) {
	c := good()
	c.Services[0].Name = "printer"
	// dns holding the fully-qualified spelling is the same name.
	dns := StaticDNS{Domain: "home.example.com", HostNames: []string{"printer.home.example.com"}}
	findError(t, Validate(c, dns, goodDevices()), "services[0].name", "would never reach the proxy")
}

func TestValidateName(t *testing.T) {
	tests := []struct{ name, want string }{
		{"", "required"},
		{"*", "the wildcard is what serves all of them"},
		{"gra fana", "letters, digits and hyphens"},
		{"-grafana", "may not begin or end with a hyphen"},
		{strings.Repeat("a", 64), "63 characters"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := good()
			c.Services[0].Name = tc.name
			findError(t, Validate(c, goodDNS(), goodDevices()), "services[0].name", tc.want)
		})
	}
}

func TestValidateRefusesDuplicateNames(t *testing.T) {
	c := good()
	c.Services = append(c.Services, Service{Name: "grafana", Upstream: Upstream{Device: "nuc", Port: 9000}})
	findError(t, Validate(c, goodDNS(), goodDevices()), "services[1].name", "already published")
}

// docs/ingress.md §1.2: a dynamic address is refused, not warned about, because
// the config works right up until a lease turns over.
func TestValidateRefusesADeviceWithoutAFixedAddress(t *testing.T) {
	c := good()
	c.Services[0].Upstream.Device = "tv"
	r := Validate(c, goodDNS(), goodDevices())
	findError(t, r, "services[0].upstream.device", "no fixed address")
	findError(t, r, "services[0].upstream.device", "olr dhcp add reservation")
}

func TestValidateUpstream(t *testing.T) {
	tests := []struct {
		name string
		up   Upstream
		path string
		want string
	}{
		{"neither", Upstream{Port: 80}, "services[0].upstream", "either a device"},
		{"both", Upstream{Device: "nuc", Host: "10.0.0.1", Port: 80}, "services[0].upstream", "alternatives"},
		{"unknown device", Upstream{Device: "ghost", Port: 80}, "services[0].upstream.device", "no device named"},
		{"no port", Upstream{Device: "nuc"}, "services[0].upstream.port", "required"},
		{"bad scheme", Upstream{Device: "nuc", Port: 80, Scheme: "ftp"}, "services[0].upstream.scheme", "unknown scheme"},
		{"host with port", Upstream{Host: "10.0.0.1:80", Port: 80}, "services[0].upstream.host", "port belongs in the port field"},
		{"unspecified", Upstream{Host: "0.0.0.0", Port: 80}, "services[0].upstream.host", "not a destination"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := good()
			c.Services[0].Upstream = tc.up
			findError(t, Validate(c, goodDNS(), goodDevices()), tc.path, tc.want)
		})
	}
}

// A literal hostname works and reintroduces a dependency the device form
// removes, so it is a remark rather than a refusal.
func TestValidateWarnsOnAHostnameUpstream(t *testing.T) {
	c := good()
	c.Services[0].Upstream = Upstream{Host: "nas.internal", Port: 5000}
	r := Validate(c, goodDNS(), goodDevices())
	if !r.OK() {
		t.Fatalf("expected valid, got %v", r.Errors)
	}
	if len(r.Warnings) != 1 || !strings.Contains(r.Warnings[0].Message, "resolved at connection time") {
		t.Fatalf("expected one propagation warning, got %v", r.Warnings)
	}
}

func TestValidateWarnsWhenEnabledAndEmpty(t *testing.T) {
	c := good()
	c.Services = nil
	r := Validate(c, goodDNS(), goodDevices())
	if !r.OK() {
		t.Fatalf("expected valid, got %v", r.Errors)
	}
	if len(r.Warnings) != 1 || !strings.Contains(r.Warnings[0].Message, "nothing is published") {
		t.Fatalf("expected the empty-but-enabled warning, got %v", r.Warnings)
	}
}

func TestValidateRefusesEscapeHatchOverreach(t *testing.T) {
	c := good()
	c.ExtraConf = "# fine\ntls internal\n"
	findError(t, Validate(c, goodDNS(), goodDevices()), "raw_caddyfile", "which this module renders")
}

func TestValidateAllowsOrdinaryEscapeHatchContent(t *testing.T) {
	c := good()
	c.ExtraConf = "status.example.com {\n\trespond \"ok\"\n}\n"
	if r := Validate(c, goodDNS(), goodDevices()); !r.OK() {
		t.Fatalf("expected valid, got %v", r.Errors)
	}
}

func TestErrNamesTheModule(t *testing.T) {
	c := good()
	c.Certificate.Provider = ""
	err := Validate(c, goodDNS(), goodDevices()).Err()
	if err == nil || !strings.Contains(err.Error(), "invalid ingress configuration") {
		t.Fatalf("got %v", err)
	}
}
