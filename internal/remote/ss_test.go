package remote

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// The proxy's rules, all of them pure. The one that earns the most space is the
// cipher/password coupling: it is the only place in this module where changing
// one field silently invalidates another, and the failure it produces is a
// server that refuses to start with a message about base64.

func proxyConfig() Config {
	c := enabledConfig()
	c.Shadowsocks = Shadowsocks{Enabled: true, Password: "3d1n8wDjnfiVvcCxXJQ4tg=="}
	return c
}

func testProxy(t *testing.T) ProxyApplier {
	t.Helper()
	dir := t.TempDir()
	store := core.NewStore(filepath.Join(dir, "olr.json"), ModuleName)
	return ProxyApplier{
		Store:  Store{Store: store},
		Paths:  Paths{Conf: filepath.Join(dir, "rendered", "shadowsocks.json")},
		Locate: func() (string, error) { return "/usr/lib/open-linux-router/ssserver", nil },
		Settle: -1,
	}
}

// --- the coupling -----------------------------------------------------------

func TestAPasswordIsGeneratedToFitItsCipher(t *testing.T) {
	for _, tc := range []struct {
		cipher Cipher
		bytes  int
	}{
		{Cipher2022AES128, 16},
		{Cipher2022AES256, 32},
		{Cipher2022ChaCha, 32},
	} {
		t.Run(string(tc.cipher), func(t *testing.T) {
			password, err := GeneratePassword(tc.cipher)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := base64.StdEncoding.DecodeString(password)
			if err != nil {
				t.Fatalf("not base64: %v", err)
			}
			if len(raw) != tc.bytes {
				t.Errorf("key is %d bytes, want %d — the server would refuse to start",
					len(raw), tc.bytes)
			}
			s := Shadowsocks{Cipher: tc.cipher, Password: password}
			if err := s.PasswordFitsCipher(); err != nil {
				t.Errorf("a generated password does not fit its own cipher: %v", err)
			}
		})
	}

	// The older ciphers take a passphrase, so any non-empty string is legal and
	// there is no length to get wrong.
	s := Shadowsocks{Cipher: CipherAES256GCM, Password: "anything at all"}
	if err := s.PasswordFitsCipher(); err != nil {
		t.Errorf("a passphrase was refused for %s: %v", CipherAES256GCM, err)
	}
}

// The trap, as a test: a password that was right for one cipher is not a
// password at all for another, and olr has to notice rather than write a file
// the server rejects.
func TestChangingTheCipherRegeneratesThePassword(t *testing.T) {
	s := Shadowsocks{Cipher: Cipher2022AES128}
	s, generated, err := s.WithGeneratedPassword()
	if err != nil {
		t.Fatal(err)
	}
	if !generated || s.Password == "" {
		t.Fatal("no password was generated for a cipher that needs one")
	}
	first := s.Password

	// Same cipher: nothing to do. A password that is regenerated on every save
	// would take every client offline once a day.
	again, generated, err := s.WithGeneratedPassword()
	if err != nil {
		t.Fatal(err)
	}
	if generated || again.Password != first {
		t.Fatal("the password was replaced by a change that did not need one")
	}

	// A longer key: the old password no longer decodes to the right length.
	s.Cipher = Cipher2022AES256
	if err := s.PasswordFitsCipher(); err == nil {
		t.Fatal("a 16-byte key was accepted for a 32-byte cipher")
	}
	s, generated, err = s.WithGeneratedPassword()
	if err != nil {
		t.Fatal(err)
	}
	if !generated || s.Password == first {
		t.Fatal("the password was kept across a cipher change that invalidates it")
	}
}

// --- what a client gets -----------------------------------------------------

func TestClientURLIsSIP002(t *testing.T) {
	c := proxyConfig()

	url, err := ClientURL(c, "home")
	if err != nil {
		t.Fatal(err)
	}
	rest, ok := strings.CutPrefix(url, "ss://")
	if !ok {
		t.Fatalf("not an ss:// link: %s", url)
	}
	userinfo, host, ok := strings.Cut(rest, "@")
	if !ok {
		t.Fatalf("no userinfo in %s", url)
	}
	if host != "home.example.net:8388#home" {
		t.Errorf("host part = %q, want the endpoint and the proxy's port", host)
	}

	decoded, err := base64.RawURLEncoding.DecodeString(userinfo)
	if err != nil {
		t.Fatalf("userinfo is not base64url: %v", err)
	}
	if want := string(DefaultCipher) + ":" + c.Shadowsocks.Password; string(decoded) != want {
		t.Errorf("userinfo decodes to %q, want %q", decoded, want)
	}
}

// Unlike the tunnel's, this can be produced again — there is one password for
// everybody and olr stores it. The asymmetry is deliberate and worth pinning.
func TestClientURLIsReproducible(t *testing.T) {
	c := proxyConfig()
	first, err := ClientURL(c, "home")
	if err != nil {
		t.Fatal(err)
	}
	second, err := ClientURL(c, "home")
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Error("two reads produced two links")
	}
}

func TestClientURLNeedsAnEndpointAndAPassword(t *testing.T) {
	c := proxyConfig()
	c.Endpoint = ""
	if _, err := ClientURL(c, ""); err == nil {
		t.Error("a link was produced with nowhere to dial")
	}

	c = proxyConfig()
	c.Shadowsocks.Password = ""
	if _, err := ClientURL(c, ""); err == nil {
		t.Error("a link was produced with no password in it")
	}
}

// --- the rendered file ------------------------------------------------------

func TestRenderedConfigCarriesUDPByDefault(t *testing.T) {
	c := proxyConfig()
	rendered, err := RenderShadowsocks(c, Paths{Conf: "/tmp/ss.json"})
	if err != nil {
		t.Fatal(err)
	}
	file, ok := rendered.Get("/tmp/ss.json")
	if !ok {
		t.Fatal("nothing rendered")
	}
	if file.Mode != 0o600 {
		t.Errorf("mode = %o, want 0600: the file holds the password", file.Mode)
	}
	if !file.Secret {
		t.Error("the file is not marked secret, so a plan would print the password")
	}

	var got map[string]any
	if err := json.Unmarshal(file.Data, &got); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	// Upstream's default is tcp_only, and the symptom of that is "the web works
	// and some apps mysteriously do not".
	if got["mode"] != "tcp_and_udp" {
		t.Errorf("mode = %v, want tcp_and_udp", got["mode"])
	}
	if got["method"] != string(DefaultCipher) {
		t.Errorf("method = %v, want %s", got["method"], DefaultCipher)
	}
	if got["server_port"] != float64(DefaultShadowsocksPort) {
		t.Errorf("server_port = %v", got["server_port"])
	}

	off := false
	c.Shadowsocks.UDP = &off
	rendered, _ = RenderShadowsocks(c, Paths{Conf: "/tmp/ss.json"})
	file, _ = rendered.Get("/tmp/ss.json")
	_ = json.Unmarshal(file.Data, &got)
	if got["mode"] != "tcp_only" {
		t.Errorf("mode = %v, want tcp_only once UDP is turned off", got["mode"])
	}
}

func TestTheEscapeHatchIsMergedAndCannotOverrideWhatWeRender(t *testing.T) {
	c := proxyConfig()
	c.Shadowsocks.ExtraConf = `{"timeout": 600}`

	rendered, err := RenderShadowsocks(c, Paths{Conf: "/tmp/ss.json"})
	if err != nil {
		t.Fatal(err)
	}
	file, _ := rendered.Get("/tmp/ss.json")
	var got map[string]any
	if err := json.Unmarshal(file.Data, &got); err != nil {
		t.Fatal(err)
	}
	if got["timeout"] != float64(600) {
		t.Errorf("the hatch was not merged: %v", got)
	}

	// The hatch is a merge rather than an append, so it *could* overwrite what
	// olr renders — and then the stored config and the running server would
	// disagree, with every surface reporting the stored one.
	c.Shadowsocks.ExtraConf = `{"server_port": 9999}`
	res := ValidateShadowsocks(c)
	if res.OK() {
		t.Fatal("the hatch was allowed to contradict the config that produced it")
	}
}

// --- validation -------------------------------------------------------------

func TestProxyValidation(t *testing.T) {
	t.Run("an unknown cipher", func(t *testing.T) {
		c := proxyConfig()
		c.Shadowsocks.Cipher = "rot13"
		if res := ValidateShadowsocks(c); res.OK() {
			t.Error("an unknown cipher was accepted")
		}
	})

	t.Run("a port the tunnel already has", func(t *testing.T) {
		c := proxyConfig()
		c.Shadowsocks.ListenPort = c.WireGuard.PortOrDefault()
		res := ValidateShadowsocks(c)
		if res.OK() {
			t.Fatal("two services were allowed onto one UDP port")
		}
		if !strings.Contains(res.Errors[0].Message, "tunnel") {
			t.Errorf("the error does not say what is already there: %q", res.Errors[0].Message)
		}
	})

	t.Run("a password that does not fit its cipher", func(t *testing.T) {
		c := proxyConfig()
		c.Shadowsocks.Cipher = Cipher2022AES256 // wants 32 bytes; the fixture has 16
		res := ValidateShadowsocks(c)
		if res.OK() {
			t.Fatal("a password too short for its cipher was accepted")
		}
		// Reported against the password, and naming the cipher — because the
		// cipher is what the operator changed.
		if !strings.Contains(res.Errors[0].Message, string(Cipher2022AES256)) {
			t.Errorf("the error does not name the cipher that decided the length: %q",
				res.Errors[0].Message)
		}
	})
}

// --- planning ---------------------------------------------------------------

func TestTurningTheProxyOnIsNotDisruptive(t *testing.T) {
	before := Config{Endpoint: "home.example.net"}
	after := proxyConfig()

	plan, _, err := BuildProxyPlan(after, before, Paths{Conf: "/tmp/ss.json"},
		ProxyObserved{Files: map[string][]byte{}})
	if err != nil {
		t.Fatal(err)
	}
	// Nobody has been handed a link, so there is nothing to invalidate. Calling
	// a first install disruptive is how an operator learns to click through.
	if plan.Impact == ImpactDisruptive {
		t.Errorf("a first install was called disruptive: %v", plan.Reasons)
	}
	if plan.Action != ActionStart {
		t.Errorf("action = %q, want start", plan.Action)
	}
}

func TestChangingThePasswordIsDisruptive(t *testing.T) {
	before := proxyConfig()
	after := before.Clone()
	after.Shadowsocks.Password = "0000000000000000000000=="

	plan, _, err := BuildProxyPlan(after, before, Paths{Conf: "/tmp/ss.json"},
		ProxyObserved{Files: map[string][]byte{}, Running: true, ServiceKnown: true})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Impact != ImpactDisruptive {
		t.Fatalf("impact = %s, want disruptive: %v", plan.Impact, plan.Reasons)
	}
	if !strings.Contains(strings.Join(plan.Reasons, " "), "every device") {
		t.Errorf("the reason does not say who has to be re-issued: %v", plan.Reasons)
	}
}

func TestTurningTheProxyOffStopsTheService(t *testing.T) {
	before := proxyConfig()
	after := before.Clone()
	after.Shadowsocks.Enabled = false

	plan, _, err := BuildProxyPlan(after, before, Paths{Conf: "/tmp/ss.json"},
		ProxyObserved{
			Files:        map[string][]byte{"/tmp/ss.json": []byte("{}")},
			Running:      true,
			ServiceKnown: true,
		})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Action != ActionStop {
		t.Errorf("action = %q, want stop", plan.Action)
	}
	// The rendered file goes away with it: there is no "valid and serves
	// nobody" form of a Shadowsocks configuration.
	if len(plan.Changes) != 1 || plan.Changes[0].Kind != FileDelete {
		t.Errorf("want the configuration removed, got %+v", plan.Changes)
	}
	if plan.Impact != ImpactDisruptive {
		t.Errorf("impact = %s, want disruptive", plan.Impact)
	}
}

// The drift check (design.md §5.4): plan unchanged intent against a box already
// holding it and the answer is nothing.
func TestAnAppliedProxyPlansEmpty(t *testing.T) {
	c := proxyConfig()
	paths := Paths{Conf: "/tmp/ss.json"}
	rendered, err := RenderShadowsocks(c, paths)
	if err != nil {
		t.Fatal(err)
	}
	obs := ProxyObserved{
		Files:         map[string][]byte{paths.Conf: rendered.Files[0].Data},
		Running:       true,
		EnabledAtBoot: true,
		ServiceKnown:  true,
	}

	plan, _, err := BuildProxyPlan(c, c, paths, obs)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Empty() {
		t.Fatalf("an applied config still has work: %+v", plan.Changes)
	}
}

// --- applying ---------------------------------------------------------------

func TestApplyWritesTheFileAndStoresIntent(t *testing.T) {
	a := testProxy(t)

	result, err := a.Apply(context.Background(), proxyConfig())
	if err != nil {
		t.Fatalf("%v\nsteps: %+v", err, result.Steps)
	}

	stored, err := a.Load()
	if err != nil {
		t.Fatal(err)
	}
	if stored.Shadowsocks.Password == "" {
		t.Error("intent was not stored")
	}

	obs, err := a.Observe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	data, ok := obs.Files[a.Paths.Conf]
	if !ok {
		t.Fatalf("nothing at %s", a.Paths.Conf)
	}
	if !strings.Contains(string(data), stored.Shadowsocks.Password) {
		t.Error("the rendered file does not carry the password the server needs")
	}
}

// --- the surface ------------------------------------------------------------

func testProxyHTTP(t *testing.T) (http.Handler, ProxyApplier) {
	t.Helper()
	tunnel, _ := testApplier(t)
	proxy := ProxyApplier{
		Store:  tunnel.Store,
		Paths:  Paths{Conf: filepath.Join(t.TempDir(), "rendered", "shadowsocks.json")},
		Locate: func() (string, error) { return "ssserver", nil },
		Settle: -1,
	}
	h := HTTP{Tunnel: tunnel, Proxy: proxy, Lock: core.NewLock(), Events: core.NewEvents()}
	return h.Handler(), proxy
}

func TestEnablingTheProxyGeneratesAPasswordAndALink(t *testing.T) {
	h, applier := testProxyHTTP(t)

	if w := do(t, h, http.MethodPatch, "/config?confirm=true",
		`{"endpoint":"home.example.net"}`); w.Code != http.StatusOK {
		t.Fatalf("endpoint = %d: %s", w.Code, w.Body)
	}
	w := do(t, h, http.MethodPatch, "/shadowsocks/config?confirm=true", `{"enabled":true}`)
	if w.Code != http.StatusOK {
		t.Fatalf("enable = %d: %s", w.Code, w.Body)
	}

	resp := decode[proxyApplyResponse](t, w)
	if !resp.Plan.PasswordGenerated {
		t.Error("the response does not say olr generated a password")
	}

	stored, _ := applier.Load()
	if err := stored.Shadowsocks.PasswordFitsCipher(); err != nil {
		t.Fatalf("the generated password does not fit the cipher: %v", err)
	}
	// Redacted on the way out, like every other credential.
	// The envelope carries `any` now that both proxies share it, and this
	// response has been through JSON, so the section arrives as a map. What
	// matters is unchanged: the credential is masked on the way out.
	section, ok := resp.Config.(map[string]any)
	if !ok || section["password"] != RedactedSecret {
		t.Errorf("the password was not redacted in the response: %+v", resp.Config)
	}

	// And the one route that hands it over deliberately.
	w = do(t, h, http.MethodGet, "/shadowsocks/link", "")
	if w.Code != http.StatusOK {
		t.Fatalf("link = %d: %s", w.Code, w.Body)
	}
	link := decode[linkResponse](t, w)
	if !strings.HasPrefix(link.URL, "ss://") {
		t.Errorf("not a link: %q", link.URL)
	}
	if !strings.Contains(link.URL, "home.example.net:8388") {
		t.Errorf("the link does not name where to dial: %q", link.URL)
	}
}

func TestTheProxyLinkIsRefusedBeforeThereIsOne(t *testing.T) {
	h, _ := testProxyHTTP(t)

	// 409 rather than 500: nothing is broken, the configuration is simply not
	// far enough along, and the message says which half is missing.
	if w := do(t, h, http.MethodGet, "/shadowsocks/link", ""); w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", w.Code, w.Body)
	}
}
