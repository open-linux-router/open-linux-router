package remote

import (
	"encoding/base64"
	"net/netip"
	"strings"
	"testing"
)

// The SOCKS5 object's rules and rendering, all pure — no files, no network, no
// root — so this whole file runs on a laptop and on macOS.

// socksConfig is a config with a working tunnel and a SOCKS5 proxy on top of
// it, which is the combination the object is designed around.
func socksConfig() Config {
	c := Config{
		Endpoint: "home.example.net",
		WireGuard: WireGuard{
			Enabled: true,
			Subnet:  netip.MustParsePrefix("10.6.0.0/24"),
		},
		Socks: Socks5{
			Enabled:  true,
			Password: "c29tZSBwYXNzd29yZA==",
		},
	}
	c.Normalize()
	return c
}

func TestSocksDefaults(t *testing.T) {
	var s Socks5
	if got := s.ListenScopeOrDefault(); got != ListenTunnel {
		t.Errorf("default listen scope = %q, want %q", got, ListenTunnel)
	}
	if got := s.PortOrDefault(); got != DefaultSocksPort {
		t.Errorf("default port = %d, want %d", got, DefaultSocksPort)
	}
	if got := s.UserOrDefault(); got != DefaultSocksUser {
		t.Errorf("default user = %q, want %q", got, DefaultSocksUser)
	}
	// The default has to be the safe one. This is the single most important
	// assertion in the file: if the zero value ever came out as ListenInternet,
	// every config that did not mention the field would expose a plaintext
	// proxy to the internet.
	if ListenScope("").Exposed() {
		t.Error("the zero listen scope is exposed; it must default to the tunnel")
	}
}

func TestSocksDialPortFollowsPublicPort(t *testing.T) {
	s := Socks5{ListenPort: 1080, PublicPort: 11080}
	if got := s.DialPort(); got != 11080 {
		t.Errorf("DialPort() = %d, want the public port 11080", got)
	}
	s.PublicPort = 0
	if got := s.DialPort(); got != 1080 {
		t.Errorf("DialPort() = %d, want the listen port 1080", got)
	}
}

func TestGenerateSocksPasswordIsUsableInAConfigLine(t *testing.T) {
	seen := map[string]bool{}
	for range 16 {
		pw, err := GenerateSocksPassword()
		if err != nil {
			t.Fatalf("GenerateSocksPassword() error: %v", err)
		}
		// 3proxy's credential line is `name:CL:secret`, split on colons and
		// whitespace, so a password containing either would render a line that
		// parses into something else.
		if strings.ContainsAny(pw, ": \t\r\n") {
			t.Fatalf("password %q contains a character that splits a 3proxy users line", pw)
		}
		if _, err := base64.StdEncoding.DecodeString(pw); err != nil {
			t.Fatalf("password %q is not base64: %v", pw, err)
		}
		if seen[pw] {
			t.Fatalf("password %q generated twice", pw)
		}
		seen[pw] = true
	}
}

func TestWithGeneratedPasswordKeepsAnExistingOne(t *testing.T) {
	// Unlike Shadowsocks, nothing about this object's other fields can
	// invalidate a password — so changing the port or the scope must not
	// re-issue credentials to everybody.
	s := Socks5{Password: "already-set"}
	out, changed, err := s.WithGeneratedPassword()
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if changed || out.Password != "already-set" {
		t.Errorf("an existing password was replaced (changed=%v, password=%q)", changed, out.Password)
	}

	out, changed, err = Socks5{}.WithGeneratedPassword()
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !changed || out.Password == "" {
		t.Error("a missing password was not generated")
	}
}

func TestValidateSocksTunnelScopeNeedsTheTunnel(t *testing.T) {
	c := socksConfig()
	c.WireGuard.Enabled = false

	r := ValidateSocks(c)
	if r.OK() {
		t.Fatal("tunnel scope with WireGuard off was accepted; it binds nothing and looks healthy")
	}
	if !hasPath(r, "socks5.listen") {
		t.Errorf("error is not attached to socks5.listen: %v", r.Errors)
	}
}

func TestTunnelScopeResolvesAnAddressEvenWithNoSubnetSet(t *testing.T) {
	// The tunnel's subnet has a default, so clearing it does *not* leave the
	// proxy with nothing to bind — which is why ValidateSocks' "no address"
	// branch is defensive rather than reachable. Asserted here so that a future
	// change making SubnetOrDefault fallible is caught by a failing test rather
	// than by a proxy that binds nothing and reports itself healthy.
	c := socksConfig()
	c.WireGuard.Subnet = netip.Prefix{}

	if r := ValidateSocks(c); !r.OK() {
		t.Fatalf("an unset subnet was refused, but it has a default: %v", r.Errors)
	}
	if !c.WireGuard.RouterAddr().IsValid() {
		t.Fatal("RouterAddr() is invalid with no subnet set; the defensive branch is now reachable")
	}
}

func TestValidateSocksInternetWarnsButDoesNotRefuse(t *testing.T) {
	c := socksConfig()
	c.Socks.Listen = ListenInternet

	r := ValidateSocks(c)
	if !r.OK() {
		t.Fatalf("exposing the proxy was refused; it must be a warning: %v", r.Errors)
	}
	if len(r.Warnings) == 0 {
		t.Fatal("exposing a plaintext proxy produced no warning")
	}
	// The warning has to name consequences and the remedy, not just say
	// "insecure" — an operator who cannot act on it will learn to scroll past.
	warning := r.Warnings[0].Message
	for _, want := range []string{"plaintext", "scanner", string(ListenTunnel)} {
		if !strings.Contains(warning, want) {
			t.Errorf("warning does not mention %q: %s", want, warning)
		}
	}
}

func TestValidateSocksTunnelScopeIsSilent(t *testing.T) {
	// The good configuration must not warn. A warning on the default is a
	// warning nobody reads.
	if r := ValidateSocks(socksConfig()); len(r.Warnings) != 0 {
		t.Errorf("the default scope produced warnings: %v", r.Warnings)
	}
}

func TestValidateSocksPortCollidesWithShadowsocksOnly(t *testing.T) {
	c := socksConfig()
	c.Shadowsocks.Enabled = true
	c.Shadowsocks.ListenPort = 1080
	c.Socks.ListenPort = 1080

	if r := ValidateSocks(c); r.OK() {
		t.Fatal("two TCP listeners on one port were accepted")
	}

	// The tunnel is UDP, so sharing its number is legal.
	c = socksConfig()
	c.WireGuard.ListenPort = 1080
	c.Socks.ListenPort = 1080
	if r := ValidateSocks(c); !r.OK() {
		t.Errorf("TCP and UDP on the same number were refused: %v", r.Errors)
	}
}

func TestValidateSocksPublicPortNeedsInternetScope(t *testing.T) {
	c := socksConfig()
	c.Socks.PublicPort = 11080

	if r := ValidateSocks(c); r.OK() {
		t.Fatal("a public port on a tunnel-scoped proxy was accepted; nothing forwards into a tunnel")
	}
}

func TestValidateSocksUsername(t *testing.T) {
	for _, bad := range []string{"has space", "has:colon", "tab\there"} {
		c := socksConfig()
		c.Socks.Username = bad
		if r := ValidateSocks(c); r.OK() {
			t.Errorf("username %q was accepted; it would split the 3proxy users line", bad)
		}
	}

	c := socksConfig()
	c.Socks.Username = "alice"
	if r := ValidateSocks(c); !r.OK() {
		t.Errorf("an ordinary username was refused: %v", r.Errors)
	}
}

func TestValidateSocksExtraCannotUndoAuthentication(t *testing.T) {
	// The hatch is appended and 3proxy reads top to bottom, so these override
	// rather than duplicate — and two of them would turn a credentialled proxy
	// into an open one.
	for _, bad := range []string{"auth none", "users bob:CL:x", "allow bob", "flush", "daemon"} {
		c := socksConfig()
		c.Socks.ExtraConf = bad
		if r := ValidateSocks(c); r.OK() {
			t.Errorf("raw_3proxy_conf %q was accepted", bad)
		}
	}

	c := socksConfig()
	c.Socks.ExtraConf = "# a comment\nmaxconn 200"
	if r := ValidateSocks(c); !r.OK() {
		t.Errorf("an innocuous hatch line was refused: %v", r.Errors)
	}
}

func TestRenderSocksBindsTheTunnelAddress(t *testing.T) {
	c := socksConfig()
	rendered, err := RenderSocks(c, RootedPaths(t.TempDir()))
	if err != nil {
		t.Fatalf("RenderSocks() error: %v", err)
	}
	if len(rendered.Files) != 1 {
		t.Fatalf("rendered %d files, want 1", len(rendered.Files))
	}

	f := rendered.Files[0]
	if f.Mode != 0o600 {
		t.Errorf("mode = %v, want 0600: the file holds a cleartext credential", f.Mode)
	}
	if !f.Secret {
		t.Error("the file is not marked secret, so its contents would be displayed in plans")
	}

	conf := string(f.Data)
	// 10.6.0.1 is the first host address of the tunnel subnet.
	if !strings.Contains(conf, "socks -p1080 -i10.6.0.1") {
		t.Errorf("did not bind the tunnel address:\n%s", conf)
	}
	if !strings.Contains(conf, "auth strong") {
		t.Errorf("authentication is not required:\n%s", conf)
	}
	if !strings.Contains(conf, "users olr:CL:c29tZSBwYXNzd29yZA==") {
		t.Errorf("the account was not rendered:\n%s", conf)
	}
	if !strings.Contains(conf, "allow olr") {
		t.Errorf("the account is not allowed, so it can authenticate and do nothing:\n%s", conf)
	}

	// The three deliberate omissions (socks_render.go). `daemon` would break
	// the unit, and a guessed UDP flag could disable authentication.
	for _, forbidden := range []string{"daemon", "nserver", " -u"} {
		if strings.Contains(conf, forbidden) {
			t.Errorf("rendered %q, which socks_render.go explains it must not:\n%s", forbidden, conf)
		}
	}
	if !strings.Contains(conf, "Generated by open-linux-router") {
		t.Errorf("no ownership header:\n%s", conf)
	}
}

func TestRenderSocksInternetScopeBindsEverything(t *testing.T) {
	c := socksConfig()
	c.Socks.Listen = ListenInternet

	rendered, err := RenderSocks(c, RootedPaths(t.TempDir()))
	if err != nil {
		t.Fatalf("RenderSocks() error: %v", err)
	}
	if conf := string(rendered.Files[0].Data); !strings.Contains(conf, "-i0.0.0.0") {
		t.Errorf("did not bind every interface:\n%s", conf)
	}
}

func TestRenderSocksAppendsTheHatchAfterTheAllowRule(t *testing.T) {
	c := socksConfig()
	c.Socks.ExtraConf = "maxconn 200"

	rendered, err := RenderSocks(c, RootedPaths(t.TempDir()))
	if err != nil {
		t.Fatalf("RenderSocks() error: %v", err)
	}
	conf := string(rendered.Files[0].Data)
	// 3proxy applies the first matching ACL, so an operator narrowing access
	// has to land after ours for it to mean anything.
	if strings.Index(conf, "maxconn 200") < strings.Index(conf, "allow olr") {
		t.Errorf("the hatch was rendered before olr's rules:\n%s", conf)
	}
}

func TestSocksClientURL(t *testing.T) {
	c := socksConfig()

	// Tunnel scope: the client dials this box *inside* the tunnel, never the
	// public endpoint — that is where the tunnel itself is dialled.
	got, err := SocksClientURL(c)
	if err != nil {
		t.Fatalf("SocksClientURL() error: %v", err)
	}
	if !strings.Contains(got, "@10.6.0.1:1080") {
		t.Errorf("tunnel-scoped URL does not dial the tunnel address: %s", got)
	}
	if strings.Contains(got, "home.example.net") {
		t.Errorf("tunnel-scoped URL points at the public endpoint: %s", got)
	}

	c.Socks.Listen = ListenInternet
	got, err = SocksClientURL(c)
	if err != nil {
		t.Fatalf("SocksClientURL() error: %v", err)
	}
	if !strings.Contains(got, "@home.example.net:1080") {
		t.Errorf("internet-scoped URL does not dial the endpoint: %s", got)
	}
}

func TestSocksClientURLEscapesTheCredential(t *testing.T) {
	c := socksConfig()
	// Base64 contains + and /, and both change meaning in a URL's userinfo.
	c.Socks.Password = "a+b/c=="

	got, err := SocksClientURL(c)
	if err != nil {
		t.Fatalf("SocksClientURL() error: %v", err)
	}
	if strings.Contains(got, "a+b/c") {
		t.Errorf("the password was not percent-encoded: %s", got)
	}
}

func TestConfigRedactsTheSocksPassword(t *testing.T) {
	c := socksConfig()
	if got := c.Redacted().Socks.Password; got != RedactedSecret {
		t.Errorf("redacted password = %q, want %q", got, RedactedSecret)
	}
	if c.Socks.Password == RedactedSecret {
		t.Error("Redacted() mutated the original config")
	}
}

func TestUnmarshalSocksRejectsUnknownFields(t *testing.T) {
	// The mistake this catches is the one that matters for this object: an
	// operator who believes they confined the proxy to the tunnel and did not.
	if _, err := UnmarshalSocks([]byte(`{"listen_scope":"tunnel"}`)); err == nil {
		t.Error("a misspelled key was accepted")
	}
	if _, err := UnmarshalSocks([]byte(`{"listen":"tunnel"}`)); err != nil {
		t.Errorf("a valid document was refused: %v", err)
	}
}

func TestEndpointNotRequiredForATunnelScopedProxy(t *testing.T) {
	c := Config{
		WireGuard: WireGuard{Enabled: true, Subnet: netip.MustParsePrefix("10.6.0.0/24")},
		Socks:     Socks5{Enabled: true, Password: "x"},
	}
	c.WireGuard.Enabled = false
	c.Socks.Listen = ListenInternet

	var r Result
	validateEndpoint(&r, c)
	if r.OK() {
		t.Error("an internet-scoped proxy with no endpoint was accepted")
	}

	// And the other way: tunnel-scoped needs no public endpoint at all.
	c.Socks.Listen = ListenTunnel
	r = Result{}
	validateEndpoint(&r, c)
	if !r.OK() {
		t.Errorf("a tunnel-scoped proxy was made to require an endpoint: %v", r.Errors)
	}
}
