package remote

import (
	"encoding/base64"
	"encoding/json"
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
	if got := s.PortOrDefault(); got != DefaultSocksPort {
		t.Errorf("default port = %d, want %d", got, DefaultSocksPort)
	}
	if got := s.UserOrDefault(); got != DefaultSocksUser {
		t.Errorf("default user = %q, want %q", got, DefaultSocksUser)
	}
}

func TestDefaultSocksRunsWithoutWireGuard(t *testing.T) {
	c := Config{Endpoint: "home.example.net", Socks: Socks5{Enabled: true, Password: "secret"}}
	if r := ValidateSocks(c); !r.OK() {
		t.Fatalf("independent proxy refused: %v", r.Errors)
	}
	plan, rendered, err := BuildSocksPlan(c, Config{}, RootedPaths(t.TempDir()), ProxyObserved{Files: map[string][]byte{}})
	if err != nil || !plan.Validation.OK() {
		t.Fatalf("independent proxy plan failed: %v, %+v", err, plan)
	}
	if !strings.Contains(string(rendered.Files[0].Data), "socks -p1080 -i0.0.0.0") {
		t.Fatal("default proxy does not listen on all interfaces")
	}
	link, err := SocksClientURL(c)
	if err != nil || !strings.Contains(link, "@home.example.net:1080") {
		t.Fatalf("independent proxy link = %q, error = %v", link, err)
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

func TestAStoredTunnelScopeIsRefusedRatherThanConverted(t *testing.T) {
	// The field is gone, and a document that still sets it must not be quietly
	// turned into a proxy the internet can reach: `tunnel` was the one value
	// that kept this listener off the public network, and applying it now would
	// undo that without anybody deciding to.
	s, err := UnmarshalSocks([]byte(`{"enabled":true,"password":"secret","listen":"tunnel"}`))
	if err != nil {
		t.Fatalf("an old document no longer loads at all: %v", err)
	}
	r := ValidateSocks(Config{Socks: s})
	if r.OK() {
		t.Fatal("a stored tunnel scope was accepted; applying it would expose a proxy that was confined")
	}
	if !hasPath(r, "socks5") {
		t.Errorf("the refusal is attached to no field: %v", r.Errors)
	}
	if !strings.Contains(r.Errors[0].Message, "internet") {
		t.Errorf("the refusal does not say what to do instead: %s", r.Errors[0].Message)
	}

	// `internet` described what the proxy still does, so it is accepted and
	// dropped rather than refused.
	s, err = UnmarshalSocks([]byte(`{"enabled":true,"password":"secret","listen":"internet"}`))
	if err != nil {
		t.Fatalf("a document saying `internet` was refused: %v", err)
	}
	if r := ValidateSocks(Config{Socks: s}); !r.OK() {
		t.Fatalf("a document saying `internet` did not validate: %v", r.Errors)
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "listen") {
		t.Errorf("the removed field is still written back: %s", data)
	}
}

func TestValidateSocksWarnsAboutTheExposure(t *testing.T) {
	c := socksConfig()

	r := ValidateSocks(c)
	if !r.OK() {
		t.Fatalf("the proxy's only shape was refused; the exposure is a warning: %v", r.Errors)
	}
	if len(r.Warnings) == 0 {
		t.Fatal("a plaintext proxy reachable from anywhere produced no warning")
	}
	// The warning has to name consequences and the remedy, not just say
	// "insecure" — an operator who cannot act on it will learn to scroll past.
	warning := r.Warnings[0].Message
	for _, want := range []string{"plaintext", "scanner", "firewall"} {
		if !strings.Contains(warning, want) {
			t.Errorf("warning does not mention %q: %s", want, warning)
		}
	}
}

func TestValidateSocksIsSilentWhileTheProxyIsOff(t *testing.T) {
	// The warning is about a proxy somebody is running. A box that has one
	// configured and switched off is not exposing anything, and a warning there
	// is the kind operators learn to ignore.
	c := socksConfig()
	c.Socks.Enabled = false

	if r := ValidateSocks(c); len(r.Warnings) != 0 {
		t.Errorf("a switched-off proxy produced warnings: %v", r.Warnings)
	}
}

func TestValidateSocksAcceptsAPublicPort(t *testing.T) {
	// A router in front forwarding 11080 to our 1080 is the case this field is
	// for, and it is meaningful in the proxy's only shape.
	c := socksConfig()
	c.Socks.PublicPort = 11080

	if r := ValidateSocks(c); !r.OK() {
		t.Fatalf("a public port was refused: %v", r.Errors)
	}
	if got := c.Socks.DialPort(); got != 11080 {
		t.Errorf("DialPort() = %d, want the public port", got)
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

func TestRenderSocksBindsEveryInterface(t *testing.T) {
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
	// Spelled out rather than left to 3proxy's default. The proxy has one
	// shape, and this is the line that says which.
	if !strings.Contains(conf, "socks -p1080 -i0.0.0.0") {
		t.Errorf("did not bind every interface:\n%s", conf)
	}
	if strings.Contains(conf, "10.6.0.1") {
		t.Errorf("the proxy still depends on the tunnel's address:\n%s", conf)
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

	got, err := SocksClientURL(c)
	if err != nil {
		t.Fatalf("SocksClientURL() error: %v", err)
	}
	if !strings.Contains(got, "@home.example.net:1080") {
		t.Errorf("the link does not dial the public endpoint: %s", got)
	}

	// The public port, when one is set: a router in front forwards it, and a
	// link naming 1080 would be a link that does not connect.
	c.Socks.PublicPort = 11080
	got, err = SocksClientURL(c)
	if err != nil {
		t.Fatalf("SocksClientURL() error: %v", err)
	}
	if !strings.Contains(got, "@home.example.net:11080") {
		t.Errorf("the link does not dial the public port: %s", got)
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
	if _, err := UnmarshalSocks([]byte(`{"listen_scope":"tunnel"}`)); err == nil {
		t.Error("a misspelled key was accepted")
	}
	if _, err := UnmarshalSocks([]byte(`{"listen":"sideways"}`)); err == nil {
		t.Error("an unknown listen value was accepted")
	}
}

func TestEndpointRequiredForASocks5ProxyWithTheTunnelOff(t *testing.T) {
	// One of the two ways in being on is enough to require the address they are
	// dialled at — the proxy needs its own, whatever the tunnel is doing.
	c := Config{Socks: Socks5{Enabled: true, Password: "x"}}

	var r Result
	validateEndpoint(&r, c)
	if r.OK() {
		t.Error("a proxy with no endpoint was accepted; its link would name no address")
	}
}

func TestBuildSocksPlanWritesOneSecretFile(t *testing.T) {
	paths := RootedPaths(t.TempDir())
	c := socksConfig()

	plan, rendered, err := BuildSocksPlan(c, Config{}, paths, ProxyObserved{Files: map[string][]byte{}})
	if err != nil {
		t.Fatalf("BuildSocksPlan() error: %v", err)
	}
	if len(plan.Changes) != 1 || plan.Changes[0].Kind != FileCreate {
		t.Fatalf("expected one create, got %+v", plan.Changes)
	}
	if !plan.Changes[0].Secret {
		t.Error("the change is not secret; its diff would print the credential")
	}
	if plan.Action != ActionStart {
		t.Errorf("action = %q, want start", plan.Action)
	}
	if got := rendered.Files[0].Path; got != paths.SocksConf {
		t.Errorf("rendered to %q, want %q", got, paths.SocksConf)
	}
}

func TestBuildSocksPlanDisabledRendersNothing(t *testing.T) {
	paths := RootedPaths(t.TempDir())
	c := socksConfig()
	c.Socks.Enabled = false

	plan, rendered, err := BuildSocksPlan(c, Config{}, paths, ProxyObserved{Files: map[string][]byte{}})
	if err != nil {
		t.Fatalf("BuildSocksPlan() error: %v", err)
	}
	if len(rendered.Files) != 0 {
		t.Errorf("a disabled proxy rendered %d files", len(rendered.Files))
	}
	if !plan.Empty() {
		t.Errorf("a disabled proxy on a fresh box planned work: %+v", plan)
	}
}

func TestChangingTheCredentialReissuesEveryLink(t *testing.T) {
	paths := RootedPaths(t.TempDir())
	before := socksConfig()
	after := before.Clone()
	after.Socks.Password = "bmV3IHBhc3N3b3Jk"

	// Pretend the old config is live and its file is on disk, so this is a
	// change to a proxy people are already using.
	rendered, err := RenderSocks(before, paths)
	if err != nil {
		t.Fatalf("RenderSocks() error: %v", err)
	}
	obs := ProxyObserved{
		Files:   map[string][]byte{paths.SocksConf: rendered.Files[0].Data},
		Running: true,
	}

	plan, _, err := BuildSocksPlan(after, before, paths, obs)
	if err != nil {
		t.Fatalf("BuildSocksPlan() error: %v", err)
	}
	if plan.Impact != ImpactDisruptive {
		t.Errorf("impact = %v, want disruptive: the link carries the password", plan.Impact)
	}
	if len(plan.Reasons) == 0 {
		t.Fatal("no reason given for a disruptive change")
	}
}

func TestProxyObjectSelectsTheRightBackend(t *testing.T) {
	if got := ObjectSocks.kind().Unit; got != SocksUnitName {
		t.Errorf("ObjectSocks drives %q, want %q", got, SocksUnitName)
	}
	paths := RootedPaths("/tmp/x")
	if got := ObjectSocks.kind().Conf(paths); got != paths.SocksConf {
		t.Errorf("ObjectSocks writes %q, want %q", got, paths.SocksConf)
	}
}
