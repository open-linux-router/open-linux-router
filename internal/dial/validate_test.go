package dial

import (
	"net/netip"
	"strings"
	"testing"
	"time"
)

func validRecord() Record {
	return Record{
		Name:         "home.example.net",
		Provider:     "cloudflare",
		Token:        "cf-token",
		Source:       SourceReflector,
		ReflectorURL: "https://reflector.example/",
	}
}

func validateOne(t *testing.T, rec Record, links LinkView) Result {
	t.Helper()
	c := Config{Records: []Record{rec}}
	c.Normalize()
	return Validate(c, links)
}

func errorAt(r Result, path string) (Problem, bool) {
	for _, p := range r.Errors {
		if p.Path == path {
			return p, true
		}
	}
	return Problem{}, false
}

func warningAt(r Result, path string) (Problem, bool) {
	for _, p := range r.Warnings {
		if p.Path == path {
			return p, true
		}
	}
	return Problem{}, false
}

func TestAValidRecordPasses(t *testing.T) {
	if res := validateOne(t, validRecord(), testLinks()); !res.OK() {
		t.Fatalf("a valid record was refused: %v", res.Errors)
	}
}

// docs/ddns.md §4.4: upstream's dispatch turns a typo into Alibaba DNS. Ours
// refuses the name and lists what exists.
func TestAnUnknownProviderIsRefused(t *testing.T) {
	rec := validRecord()
	rec.Provider = "clouflare"

	res := validateOne(t, rec, testLinks())
	p, found := errorAt(res, "records[0].provider")
	if !found {
		t.Fatalf("a misspelled provider was accepted: %+v", res)
	}
	if !strings.Contains(p.Message, "cloudflare") {
		t.Errorf("the error does not list what exists: %q", p.Message)
	}
}

// Two of the four providers need a key pair, and learning that from a 401 three
// hours later is worse than learning it from the empty field.
func TestKeyPairProvidersNeedBothHalves(t *testing.T) {
	for _, name := range []ProviderName{"alidns", "tencentcloud"} {
		rec := validRecord()
		rec.Provider = name
		rec.Token = "secret"

		res := validateOne(t, rec, testLinks())
		if _, found := errorAt(res, "records[0].provider_key_id"); !found {
			t.Errorf("%s accepted a secret with no key ID", name)
		}

		rec.KeyID = "id"
		if res := validateOne(t, rec, testLinks()); !res.OK() {
			t.Errorf("%s refused a complete key pair: %v", name, res.Errors)
		}
	}
}

func TestCloudflareRefusesAKeyID(t *testing.T) {
	rec := validRecord()
	rec.KeyID = "not-a-thing"

	if _, found := errorAt(validateOne(t, rec, testLinks()), "records[0].provider_key_id"); !found {
		t.Fatal("cloudflare authenticates with a token alone; a key ID should be refused")
	}
}

func TestCallbackNeedsItsURL(t *testing.T) {
	rec := validRecord()
	rec.Provider = "callback"
	rec.Token = ""

	if _, found := errorAt(validateOne(t, rec, testLinks()), "records[0].callback_url"); !found {
		t.Fatal("callback with no URL was accepted")
	}

	rec.CallbackURL = "https://www.duckdns.org/update?domains=home&token=t&ip=#{ip}"
	if res := validateOne(t, rec, testLinks()); !res.OK() {
		t.Fatalf("a complete callback record was refused: %v", res.Errors)
	}
}

func TestCallbackWarnsWhenTheAddressIsNeverSent(t *testing.T) {
	rec := validRecord()
	rec.Provider = "callback"
	rec.Token = ""
	rec.CallbackURL = "https://example.net/hook"

	res := validateOne(t, rec, testLinks())
	if !res.OK() {
		t.Fatalf("this is legal — some endpoints read the address off the connection: %v", res.Errors)
	}
	if _, found := warningAt(res, "records[0].callback_url"); !found {
		t.Error("a callback that never mentions the address should be questioned")
	}
}

// design.md §5.6, and the reason this enum has no default.
func TestTheSourceMustBeDeclared(t *testing.T) {
	rec := validRecord()
	rec.Source = ""

	p, found := errorAt(validateOne(t, rec, testLinks()), "records[0].source")
	if !found {
		t.Fatal("a record with no source was accepted")
	}
	for _, want := range []string{"interface", "reflector"} {
		if !strings.Contains(p.Message, want) {
			t.Errorf("the error does not offer %q: %q", want, p.Message)
		}
	}
}

func TestBothSourcesSetIsRefused(t *testing.T) {
	rec := validRecord()
	rec.Source = SourceInterface
	rec.Interface = "eth0"
	rec.ReflectorURL = "https://reflector.example/"

	if _, found := errorAt(validateOne(t, rec, testLinks()), "records[0].reflector_url"); !found {
		t.Fatal("an interface-sourced record carrying a reflector URL was accepted")
	}
}

func TestTheReflectorSourceNeedsAnEndpoint(t *testing.T) {
	rec := validRecord()
	rec.ReflectorURL = ""

	p, found := errorAt(validateOne(t, rec, testLinks()), "records[0].reflector_url")
	if !found {
		t.Fatal("a reflector-sourced record with no endpoint was accepted")
	}
	// docs/ddns.md §9 #3: shipping a default would point every installation at
	// one operator's endpoint, and the message says so rather than leaving the
	// absence looking like an oversight.
	if !strings.Contains(p.Message, "default") {
		t.Errorf("the error does not say why there is no default: %q", p.Message)
	}
}

func TestTheInterfaceSourceNeedsAnInterface(t *testing.T) {
	rec := validRecord()
	rec.Source = SourceInterface
	rec.ReflectorURL = ""

	if _, found := errorAt(validateOne(t, rec, testLinks()), "records[0].interface"); !found {
		t.Fatal("an interface-sourced record naming no interface was accepted")
	}
}

// The adopt-only rule (design.md §3.4), enforced against the field that named
// the interface.
func TestAnUnadoptedInterfaceIsRefused(t *testing.T) {
	rec := validRecord()
	rec.Source = SourceInterface
	rec.ReflectorURL = ""
	rec.Interface = "eth2"

	p, found := errorAt(validateOne(t, rec, testLinks()), "records[0].interface")
	if !found {
		t.Fatal("an unadopted interface was accepted")
	}
	if !strings.Contains(p.Message, "olr adopt eth2") {
		t.Errorf("the error does not say how to fix it: %q", p.Message)
	}
}

func TestAMissingInterfaceIsRefused(t *testing.T) {
	rec := validRecord()
	rec.Interface = "wan99"

	if _, found := errorAt(validateOne(t, rec, testLinks()), "records[0].interface"); !found {
		t.Fatal("binding to an interface this machine does not have was accepted")
	}
}

// The reference topology's trap (docs/ddns.md §3.1). A warning, not an error:
// olr must not switch the operator to the reflector form on their behalf.
func TestAPrivateAddressOnTheUplinkIsAWarning(t *testing.T) {
	rec := validRecord()
	rec.Source = SourceInterface
	rec.ReflectorURL = ""
	rec.Interface = "eth1"

	res := validateOne(t, rec, testLinks())
	if !res.OK() {
		t.Fatalf("a private uplink address is legitimate in a split-horizon zone: %v", res.Errors)
	}
	p, found := warningAt(res, "records[0].interface")
	if !found {
		t.Fatal("publishing a private address should be questioned")
	}
	if !strings.Contains(p.Message, string(SourceReflector)) {
		t.Errorf("the warning does not point at the other source: %q", p.Message)
	}
}

// A floor rather than a preference: the free reflectors are free on the
// understanding that nobody polls them.
func TestTheIntervalHasAFloor(t *testing.T) {
	rec := validRecord()
	rec.Interval = Duration(time.Second)

	if _, found := errorAt(validateOne(t, rec, testLinks()), "records[0].interval"); !found {
		t.Fatal("a one-second interval was accepted")
	}

	rec.Interval = Duration(MinInterval)
	if res := validateOne(t, rec, testLinks()); !res.OK() {
		t.Errorf("the floor itself was refused: %v", res.Errors)
	}
}

func TestANonASCIINameIsRefused(t *testing.T) {
	rec := validRecord()
	rec.Name = "家.example.net"

	p, found := errorAt(validateOne(t, rec, testLinks()), "records[0].name")
	if !found {
		t.Fatal("a non-ASCII name was accepted; the providers' APIs expect punycode")
	}
	if !strings.Contains(p.Message, "xn--") {
		t.Errorf("the error does not say what to type instead: %q", p.Message)
	}
}

// A name that is itself a public suffix leaves nothing to be the registered
// domain, so there is no zone to address the record in.
func TestANameWithNoDerivableZoneAsksForOne(t *testing.T) {
	rec := validRecord()
	rec.Name = "co.uk"

	if _, found := errorAt(validateOne(t, rec, testLinks()), "records[0].zone"); !found {
		t.Fatal("a name whose zone cannot be derived was accepted; " +
			"the provider's 'no such zone' reads like a credential problem")
	}

	rec.Zone = "co.uk"
	if res := validateOne(t, rec, testLinks()); !res.OK() {
		t.Errorf("an explicit zone did not settle it: %v", res.Errors)
	}
}

// A split-horizon zone under an unlisted suffix is an ordinary setup and gets
// an answer rather than a refusal.
func TestAnUnlistedSuffixStillDerivesAZone(t *testing.T) {
	rec := validRecord()
	rec.Name = "home.example.internal"

	if res := validateOne(t, rec, testLinks()); !res.OK() {
		t.Fatalf("a name under an unlisted suffix was refused: %v", res.Errors)
	}
}

// Two records for one name would each publish over the other, and each would
// then see the other's write as an address change — so the pair would call the
// provider on every check, forever.
func TestDuplicateNamesAreRefused(t *testing.T) {
	c := Config{Records: []Record{validRecord(), validRecord()}}
	c.Normalize()

	res := Validate(c, testLinks())
	if res.OK() {
		t.Fatal("two records for one name were accepted")
	}
}

// design.md §5.3.1: the rules that do not need the box stay checkable without
// one.
func TestValidationWorksWithoutInterfaceFacts(t *testing.T) {
	rec := validRecord()
	rec.Provider = "nonesuch"

	res := validateOne(t, rec, nil)
	if res.OK() {
		t.Fatal("the vocabulary rules should still apply with no link view")
	}
}

// --- the uplink -------------------------------------------------------------

// uplinkLinks is the three-NIC box from the report this object was built for:
// one LAN, one facing the fibre modem, and one carrying a second subnet.
func uplinkLinks() StaticView {
	return StaticView{
		Links: StaticLinks{
			"enp1s0": {Adopted: true, Up: true, Prefixes: []netip.Prefix{
				netip.MustParsePrefix("192.168.1.1/24"),
			}},
			"enp2s0": {Adopted: true, Up: true},
			"enp3s0": {Adopted: false, Up: true},
		},
		Nets: []NetworkInfo{{Name: "lan", Members: []string{"enp1s0"}}},
	}
}

func validateUplinkOnly(t *testing.T, u Uplink, links LinkView) Result {
	t.Helper()
	c := Config{Uplink: &u}
	c.Normalize()
	return Validate(c, links)
}

func validUplink() Uplink {
	return Uplink{
		Interface: "enp2s0",
		IPv4: &UplinkIPv4{
			Address: netip.MustParsePrefix("192.168.2.9/24"),
			Gateway: netip.MustParseAddr("192.168.2.1"),
		},
	}
}

func TestAValidUplinkPasses(t *testing.T) {
	if res := validateUplinkOnly(t, validUplink(), uplinkLinks()); !res.OK() {
		t.Fatalf("a valid uplink was refused: %v", res.Errors)
	}
}

// The reporter's migration path, and the reason this refusal exists: before
// dial.Uplink there was nowhere else to put a WAN NIC, so an existing box has
// it in a network — and internal/link would strip the ISP's address off it.
func TestAnUplinkOnAnInterfaceANetworkCarriesIsRefused(t *testing.T) {
	u := validUplink()
	u.Interface = "enp1s0"

	res := validateUplinkOnly(t, u, uplinkLinks())
	p, found := errorAt(res, UplinkPath+".interface")
	if !found {
		t.Fatalf("an uplink on a network's interface was allowed: %+v", res)
	}
	// It has to name the network and the way out, or the operator cannot act
	// on it: "remove the network first" is the whole value of the message.
	for _, want := range []string{"lan", "olr net rm lan"} {
		if !strings.Contains(p.Message, want) {
			t.Errorf("the refusal does not mention %q: %s", want, p.Message)
		}
	}
}

func TestAnUnadoptedUplinkInterfaceIsRefused(t *testing.T) {
	u := validUplink()
	u.Interface = "enp3s0"

	res := validateUplinkOnly(t, u, uplinkLinks())
	p, found := errorAt(res, UplinkPath+".interface")
	if !found {
		t.Fatalf("an unadopted interface was accepted: %+v", res)
	}
	if !strings.Contains(p.Message, "olr adopt enp3s0") {
		t.Errorf("the refusal does not say how to fix it: %s", p.Message)
	}
}

func TestAMissingUplinkInterfaceIsRefused(t *testing.T) {
	u := validUplink()
	u.Interface = "enp9s0"

	if _, found := errorAt(validateUplinkOnly(t, u, uplinkLinks()), UplinkPath+".interface"); !found {
		t.Error("an interface this machine does not have was accepted")
	}
}

// The field that had nowhere to live. An address with no gateway is the state
// the reporter was stuck in, so it is an error rather than a warning.
func TestAnUplinkAddressWithoutAGatewayIsRefused(t *testing.T) {
	u := validUplink()
	u.IPv4.Gateway = netip.Addr{}

	if _, found := errorAt(validateUplinkOnly(t, u, uplinkLinks()), UplinkPath+".ipv4.gateway"); !found {
		t.Error("an address with no gateway was accepted")
	}
}

// An off-link next hop would need a route to itself first, and olr writes
// none — the kernel refuses with ENETUNREACH halfway through an apply, which
// is a much worse place to learn that the mask is wrong.
func TestAGatewayOutsideTheAddressIsRefused(t *testing.T) {
	u := validUplink()
	u.IPv4.Gateway = netip.MustParseAddr("10.9.9.1")

	res := validateUplinkOnly(t, u, uplinkLinks())
	p, found := errorAt(res, UplinkPath+".ipv4.gateway")
	if !found {
		t.Fatalf("a gateway outside the subnet was accepted: %+v", res)
	}
	if !strings.Contains(p.Message, "192.168.2.0/24") {
		t.Errorf("the refusal does not name the subnet: %s", p.Message)
	}
}

func TestTheGatewayMayNotBeThisBoxsOwnAddress(t *testing.T) {
	u := validUplink()
	u.IPv4.Gateway = netip.MustParseAddr("192.168.2.9")

	if _, found := errorAt(validateUplinkOnly(t, u, uplinkLinks()), UplinkPath+".ipv4.gateway"); !found {
		t.Error("the box's own address was accepted as its gateway")
	}
}

// Normalize deliberately does not mask the address, so the typo has to be
// caught rather than quietly corrected into something unreachable.
func TestTheNetworkAddressIsRefusedAsTheUplinkAddress(t *testing.T) {
	u := validUplink()
	u.IPv4.Address = netip.MustParsePrefix("192.168.2.0/24")

	res := validateUplinkOnly(t, u, uplinkLinks())
	p, found := errorAt(res, UplinkPath+".ipv4.address")
	if !found {
		t.Fatalf("the network address was accepted as an interface address: %+v", res)
	}
	if !strings.Contains(p.Message, "192.168.2.1/24") {
		t.Errorf("the refusal does not show the shape of a right answer: %s", p.Message)
	}
}

// Double NAT is a working setup, so this is a warning — internal/gateway/nat makes
// the same call about the same fact.
func TestAPrivateUplinkAddressIsAWarning(t *testing.T) {
	res := validateUplinkOnly(t, validUplink(), uplinkLinks())
	if !res.OK() {
		t.Fatalf("a private uplink address was refused: %v", res.Errors)
	}
	if _, found := warningAt(res, UplinkPath+".ipv4.address"); !found {
		t.Errorf("no note about being behind another router: %+v", res.Warnings)
	}
}

func TestAPublicUplinkAddressIsNotWarnedAbout(t *testing.T) {
	u := validUplink()
	u.IPv4.Address = netip.MustParsePrefix("203.0.113.17/29")
	u.IPv4.Gateway = netip.MustParseAddr("203.0.113.22")

	res := validateUplinkOnly(t, u, uplinkLinks())
	if !res.OK() {
		t.Fatalf("a public uplink address was refused: %v", res.Errors)
	}
	if _, found := warningAt(res, UplinkPath+".ipv4.address"); found {
		t.Error("a public address should not be warned about")
	}
}

// The resolvers are this router's own when the uplink is static — and only
// recorded when it is not, which is the case worth saying where it is typed.
func TestResolversSayWhenTheyAreNotUsed(t *testing.T) {
	u := validUplink()
	u.DNS = []netip.Addr{netip.MustParseAddr("9.9.9.9")}

	res := validateUplinkOnly(t, u, uplinkLinks())
	if !res.OK() {
		t.Fatalf("resolvers were refused: %v", res.Errors)
	}
	if w, found := warningAt(res, UplinkPath+".dns"); found {
		t.Errorf("a static uplink's resolvers were called unused: %s", w.Message)
	}

	bare := Uplink{Interface: u.Interface, DNS: u.DNS}
	if _, found := warningAt(validateUplinkOnly(t, bare, uplinkLinks()), UplinkPath+".dns"); !found {
		t.Error("resolvers on an uplink olr does not address were not called unused")
	}
}

// An interface olr owns and does not address is the shape the DHCP-client and
// PPPoE forms arrive in, so it has to be storable now.
func TestAnUplinkWithNoStaticAddressingIsAllowed(t *testing.T) {
	res := validateUplinkOnly(t, Uplink{Interface: "enp2s0"}, uplinkLinks())
	if !res.OK() {
		t.Fatalf("an uplink with no IPv4 block was refused: %v", res.Errors)
	}
}

func TestAnUplinkNeedsAnInterface(t *testing.T) {
	if _, found := errorAt(validateUplinkOnly(t, Uplink{}, uplinkLinks()), UplinkPath+".interface"); !found {
		t.Error("an uplink with no interface was accepted")
	}
}

// design.md §5.3.1: the rules that need the box are skipped without it, and
// the rest still run.
func TestUplinkValidationWorksWithoutInterfaceFacts(t *testing.T) {
	u := validUplink()
	u.IPv4.Gateway = netip.MustParseAddr("10.9.9.1")

	res := validateUplinkOnly(t, u, nil)
	if _, found := errorAt(res, UplinkPath+".ipv4.gateway"); !found {
		t.Error("the pure rules stopped running without a LinkView")
	}
}
