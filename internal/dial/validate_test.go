package dial

import (
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
