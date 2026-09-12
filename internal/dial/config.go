// Package dial owns this box's uplink, and for now owns one thing about it:
// keeping a public name pointing at the address the uplink has today.
//
// # What this is, and what it is not
//
// design.md §9 owes a `dial` module that brings the uplink up — PPPoE, a DHCP
// client, whatever the ISP wants — and holds the facts about it that everything
// downstream reads. This is not that module, in exactly the way internal/link
// is not the module design.md §9 milestone 1 owes.
//
// It exists because docs/ddns.md decided that dynamic DNS belongs to `dial`
// (§2: the module that owns the fact owns the feature, and the value published
// is the uplink's public address), and then had to close §9 #4 — whether `dial`
// exists before DDNS does. The honest options were to wait or to build the
// smallest piece of `dial` that owns an uplink address. This is the second.
//
// So, deliberately shaped so the real module can absorb it rather than collide
// with it:
//
//   - **There is no uplink object.** A record says where its own address comes
//     from. The reference topology (docs/dns.md §1) puts olr behind the modem
//     with no WAN uplink at all, so a `dial.uplink` field would be empty on the
//     deployment this product leads with — and inventing one now would model
//     the case we do not lead with, in the module least able to change it
//     later. internal/link's package comment refuses a primary key for the same
//     reason and in the same words.
//   - **The address is read, never stored.** Which interface a record reads is
//     intent; what address that interface has is a fact, and it is read through
//     LinkView per check (§4.5). There is no copy to drift.
//   - **Nothing here supervises a backend.** This is the first module since
//     `link` with no unit to drive and no file to render, and the publisher runs
//     inside olrd the way `gateway`'s exit prober does. design.md §3.5's test
//     decides that: if olrd is stopped, the name stops being *updated*, which is
//     a loss of freshness rather than a loss of service.
//
// The name runs ahead of the contents, and that is the cost stated rather than
// hidden: `dial` contains DDNS and nothing else today.
package dial

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"golang.org/x/net/publicsuffix"

	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/dial/provider"
)

// ModuleName is the path segment, config section and event label for this
// module.
const ModuleName = "dial"

// DefaultInterval is how often a record's address is re-read when it does not
// say.
//
// Five minutes is upstream ddns-go's default and is the right order of
// magnitude for both halves of the tradeoff: an address that changed is stale
// for at most that long, and a reflector is asked 288 times a day rather than
// 1440. Note what it is *not* the frequency of — the provider is called only
// when the address changed (docs/ddns.md §6), so this is the rate of checking,
// not of publishing.
const DefaultInterval = 5 * time.Minute

// MinInterval is the fastest a record may be checked.
//
// A floor rather than a preference. The reflector form sends a request to
// somebody else's endpoint on this timer, and the ones that are free are free
// on the understanding that nobody polls them every second. An operator who
// types 1s gets a refusal here rather than a ban there.
const MinInterval = 30 * time.Second

// Config is the dial module's intent.
//
// One field today, and a list of structs rather than of names — the opposite of
// internal/link's bet, and for the opposite reason: a DDNS record has eight
// fields the day it is written, so a list of names would have to become a list
// of structs immediately.
type Config struct {
	// Records are the public names this box keeps current.
	Records []Record `json:"records,omitempty"`
}

// Record is one public name whose value this box is responsible for keeping
// current (docs/ddns.md §1).
//
// Deliberately not "a DDNS account" and not "a provider": the operator's
// sentence is *"I want home.example.net to keep pointing at me"*, and the
// provider is an implementation detail of keeping that promise.
type Record struct {
	// Name is the fully-qualified public name — "home.example.net". It is this
	// record's identity, so it is what the item routes and the CLI address it
	// by (docs/cli.md R2).
	//
	// A name with non-ASCII labels has to be given in its punycode form
	// ("xn--…"). Converting would mean carrying golang.org/x/text for the
	// handful of operators who have one; the validator says so rather than
	// letting the provider answer "no such record" about a name that looks
	// right.
	Name string `json:"name"`

	// Zone is the registered domain Name sits inside — "example.net". Empty
	// means "work it out from Name", which is right for every ordinary name and
	// wrong for exactly the cases a public suffix list cannot know about: a
	// zone delegated below a registrar's own suffix, or one the list has not
	// caught up with. ZoneOrDerived resolves it.
	//
	// It is a field rather than a pure derivation because every provider here
	// addresses a record as zone-plus-the-labels-below-it, and getting the
	// split wrong produces "no such zone" — an error that reads like a
	// credential problem and is not one.
	Zone string `json:"zone,omitempty"`

	// Provider names the DNS provider this record lives at. The vocabulary is
	// provider.Names(), and a name outside it is refused rather than defaulted
	// (docs/ddns.md §4.4).
	Provider ProviderName `json:"provider"`

	// KeyID is the non-secret half of a two-part credential: an AccessKeyId at
	// Alibaba Cloud, a SecretId at Tencent Cloud. Cloudflare's token stands
	// alone and callback has no credential of its own, so both leave this
	// empty.
	//
	// docs/ddns.md §5 talks about "the credential" in the singular because
	// Cloudflare's is, and two of the four providers v1 ships need a pair. That
	// is the only place the document and this struct differ.
	KeyID string `json:"provider_key_id,omitempty"`

	// Token is the secret half of the credential, and for callback it is the
	// request body sent to the endpoint.
	//
	// It carries the obligations docs/ingress.md §4.1 states for the one other
	// third-party credential olr holds, because the failure mode is identical:
	// nothing breaks when a token is printed, it just ends up in a scrollback
	// buffer and then in a bug report. So it is redacted on every surface that
	// prints config or a plan (see Redacted), and the mask means "unchanged" on
	// the way back in (http.go).
	//
	// Scope it to the one zone where the provider allows it. The blast radius
	// of an account-wide key here is the operator's whole DNS.
	Token string `json:"provider_token,omitempty"`

	// CallbackURL is the request URL for the `callback` provider, with `#{ip}`
	// and the other placeholders docs say it takes. Unused by the others.
	//
	// Redacted alongside Token, which is worth justifying because a URL does
	// not look like a credential: DuckDNS, Dynu and most of the DynDNS v2
	// endpoints put the token *in the query string*, so for this provider the
	// URL is the credential. Redacting it costs an operator the ability to read
	// back what they typed, which is exactly what they already cannot do with
	// Token.
	CallbackURL string `json:"callback_url,omitempty"`

	// Source says where the address comes from, and there are two because
	// neither is right everywhere (docs/ddns.md §3.1). It is declared and never
	// inferred: choosing `reflector` because the interface's address "looks
	// private" is precisely the automatic behaviour design.md §5.6 forbids, and
	// it would silently turn a local-only box into one that talks to a stranger
	// every five minutes.
	Source Source `json:"source"`

	// Interface names the uplink, and means one thing in both forms: *which way
	// out*. With SourceInterface it is the interface whose address is
	// published; with SourceReflector it is the interface the question is asked
	// through, which is optional and is what stops a multi-WAN box publishing
	// whichever answer it happened to get (docs/ddns.md §3.3).
	Interface string `json:"interface,omitempty"`

	// ReflectorURL is an HTTPS endpoint that echoes the source address back.
	// Required by SourceReflector and refused by SourceInterface.
	//
	// There is no default, and that is a decision rather than an omission:
	// shipping one would mean every olr installation reporting its address to
	// one operator's endpoint on a timer. docs/ddns.md §9 #3 leaves the choice
	// open, and the honest interim is that the operator names the third party
	// they are willing to talk to.
	ReflectorURL string `json:"reflector_url,omitempty"`

	// Interval is how often the address is re-read. Empty means
	// DefaultInterval.
	Interval Duration `json:"interval,omitempty"`

	// TTL is the published record's time to live, in seconds. Zero means the
	// provider's own default, which differs between them and is not ours to
	// unify.
	TTL int `json:"ttl,omitempty"`
}

// ProviderName is one of the DNS providers this build can publish through.
//
// A named type rather than a bare string so that schema.go can publish the
// vocabulary as an enum, which is what turns it into a typed union in
// TypeScript — and so the UI's exhaustive switches fail at compile time when a
// provider is added rather than silently at runtime on the one screen nobody
// tests. The values themselves live in the provider package, beside the code
// that implements them.
type ProviderName string

func (p ProviderName) String() string { return string(p) }

// Known reports whether this build implements the provider.
func (p ProviderName) Known() bool { return provider.Known(string(p)) }

// ProviderNames is the vocabulary, as this package's type.
func ProviderNames() []ProviderName {
	names := provider.Names()
	out := make([]ProviderName, 0, len(names))
	for _, n := range names {
		out = append(out, ProviderName(n))
	}
	return out
}

// Source is where a record's address is read from.
type Source string

// The two sources (docs/ddns.md §3.1).
const (
	// SourceInterface reads the address off the adopted uplink. Correct when
	// olr terminates the WAN — PPPoE, or DHCP from the ISP.
	SourceInterface Source = "interface"

	// SourceReflector asks an HTTPS endpoint what address it sees. Correct when
	// olr is behind the modem, which is the reference topology, and the only
	// form that can notice carrier-grade NAT (docs/ddns.md §3.2).
	SourceReflector Source = "reflector"
)

// Sources lists the vocabulary. One source, so the enum published in the schema
// and the check in the validator cannot disagree.
func Sources() []Source { return []Source{SourceInterface, SourceReflector} }

// Valid reports whether s names a source.
//
// The empty string is *not* valid, unlike every other enum in this tree. There
// is no sensible default to fall back to: one of the two answers talks to a
// stranger and the other does not, and picking either on the operator's behalf
// is the inference docs/ddns.md §3.1 refuses.
func (s Source) Valid() bool { return slices.Contains(Sources(), s) }

// ResolvedInterval fills in the default.
func (r Record) ResolvedInterval() time.Duration {
	if r.Interval <= 0 {
		return DefaultInterval
	}
	return r.Interval.Duration()
}

// ZoneOrDerived returns the zone this record's name sits in.
//
// Derived with the public suffix list when the operator did not say, which is
// the same answer ddns-go reaches and is right for "home.example.net" and
// "home.example.co.uk" alike. A name under a suffix the list does not carry
// still gets an answer — an unlisted final label is treated as the suffix — so
// "home.example.internal" derives "example.internal", which is what an operator
// with a split-horizon zone means.
//
// It returns empty only when there is no label left to be the registered
// domain: the name is itself a public suffix ("co.uk"), or has one label. The
// validator then asks for the field, because the alternative first failure is
// "no such zone" from a provider, which reads like a credential problem.
func (r Record) ZoneOrDerived() string {
	if r.Zone != "" {
		return r.Zone
	}
	zone, err := publicsuffix.EffectiveTLDPlusOne(r.Name)
	if err != nil {
		return ""
	}
	return zone
}

// ProviderRecord renders this record as the provider package's request, given
// the address to publish.
//
// The one place the module's vocabulary is translated into the providers', so
// that no provider has to know what a Source is and this package does not have
// to know that Cloudflare wants a bearer token.
func (r Record) ProviderRecord(address string) provider.Record {
	return provider.Record{
		Name:  r.Name,
		Zone:  r.ZoneOrDerived(),
		Type:  RecordType,
		Value: address,
		TTL:   r.TTL,
		KeyID: r.KeyID,
		Token: r.Token,
		URL:   r.CallbackURL,
	}
}

// RecordType is what v1 publishes.
//
// A constant rather than a field: docs/ddns.md §9 #2 has not decided what an
// AAAA means on a router — with a delegated prefix the interesting address is
// usually a *device's*, not the gateway's, which turns one field into a record
// set and pulls in `devices`. A type field offering AAAA before that is settled
// would be a promise three generated surfaces make and the validator refuses.
const RecordType = "A"

// --- collection -------------------------------------------------------------

// Find returns a record by name.
func (c Config) Find(name string) (Record, bool) {
	name = normalizeName(name)
	i := slices.IndexFunc(c.Records, func(r Record) bool { return r.Name == name })
	if i < 0 {
		return Record{}, false
	}
	return c.Records[i], true
}

// SetRecord adds or replaces a record, keyed by name.
func (c *Config) SetRecord(r Record) {
	r.Name = normalizeName(r.Name)
	if i := slices.IndexFunc(c.Records, func(e Record) bool { return e.Name == r.Name }); i >= 0 {
		c.Records[i] = r
	} else {
		c.Records = append(c.Records, r)
	}
	c.Normalize()
}

// RemoveRecord drops a record, reporting whether there was one.
//
// Note what it does not do: it does not delete the record at the provider. The
// name keeps resolving to whatever was last published, and stops being kept
// current. That is the honest behaviour — olr did not create the zone and does
// not own it — and print.go says so, because "removed" would otherwise read as
// "the name is gone".
func (c *Config) RemoveRecord(name string) bool {
	name = normalizeName(name)
	i := slices.IndexFunc(c.Records, func(r Record) bool { return r.Name == name })
	if i < 0 {
		return false
	}
	c.Records = slices.Delete(c.Records, i, i+1)
	return true
}

// Names lists the configured records, in stored order.
func (c Config) Names() []string {
	out := make([]string, 0, len(c.Records))
	for _, r := range c.Records {
		out = append(out, r.Name)
	}
	return out
}

// Empty reports whether anything is configured at all.
func (c Config) Empty() bool { return len(c.Records) == 0 }

// --- canonical form ---------------------------------------------------------

// normalizeName reduces the spellings of one name to the stored one.
//
// Lower-cased and stripped of a trailing dot, because DNS names are
// case-insensitive and "home.example.net." and "home.example.net" are the same
// name. Storing whichever was typed would let both exist at once, and then two
// records would publish to one name and each would report the other's writes as
// a change.
func normalizeName(name string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(name)), ".")
}

// Normalize puts the config in canonical form.
//
// Sorting matters for the reason it does in every other module: the document is
// compared as bytes downstream, so a reordered array would read as a change
// nobody made.
func (c *Config) Normalize() {
	for i := range c.Records {
		r := &c.Records[i]
		r.Name = normalizeName(r.Name)
		r.Zone = normalizeName(r.Zone)
		r.Provider = ProviderName(strings.TrimSpace(strings.ToLower(string(r.Provider))))
		r.Source = Source(strings.TrimSpace(strings.ToLower(string(r.Source))))
		r.Interface = strings.TrimSpace(r.Interface)
		r.ReflectorURL = strings.TrimSpace(r.ReflectorURL)
		r.CallbackURL = strings.TrimSpace(r.CallbackURL)
		r.KeyID = strings.TrimSpace(r.KeyID)
		// Token is neither trimmed nor cased. It is an opaque credential, and
		// "helpfully" altering it produces an authentication failure whose cause
		// is invisible in every diff, because the diff looks right.
	}
	slices.SortStableFunc(c.Records, func(a, b Record) int {
		return strings.Compare(a.Name, b.Name)
	})
}

// Clone returns a deep copy, so a proposal can be edited without disturbing the
// one held by the store.
func (c Config) Clone() Config {
	return Config{Records: slices.Clone(c.Records)}
}

// Redacted returns a copy safe to print, log, or return over the API.
//
// Applied by every surface rather than by the ones that looked risky. Both
// credential-bearing fields go, including the callback URL — see
// Record.CallbackURL for why a URL is one of them.
func (c Config) Redacted() Config {
	out := c.Clone()
	for i := range out.Records {
		if out.Records[i].Token != "" {
			out.Records[i].Token = RedactedToken
		}
		if out.Records[i].CallbackURL != "" {
			out.Records[i].CallbackURL = RedactedToken
		}
	}
	return out
}

// RedactedToken is what stands in for a credential on a printed surface.
//
// A fixed string rather than a length-preserving mask, so it cannot be mistaken
// for the real value and gives away nothing about it. Spelled the same way
// internal/ingress spells it, because an operator who has seen one has seen
// both.
const RedactedToken = "********"

// --- document ---------------------------------------------------------------

// UnmarshalConfig parses a config, rejecting unknown fields.
//
// Strict for the reason internal/dhcp/config.go gives, which bites in a
// particular way here: a misspelled `provider_token` is silently dropped, the
// old credential keeps working until it is rotated, and the failure surfaces
// weeks later as a name that stopped updating.
func UnmarshalConfig(data []byte) (Config, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()

	var c Config
	if err := dec.Decode(&c); err != nil {
		return Config{}, fmt.Errorf("%s configuration: %w", ModuleName, err)
	}
	c.Normalize()
	return c, nil
}

// MarshalConfig renders this module's subtree of the document.
//
// No "$schema" key: the document has exactly one, written by the store.
// Indentation is likewise the store's — it re-indents the whole document in one
// pass.
func MarshalConfig(c Config) ([]byte, error) {
	c.Normalize()
	return json.Marshal(c)
}

// FromDocument reads this module's subtree out of the configuration document.
//
// A document without a "dial" key is not an error — it means no name is being
// published, which is what a fresh install looks like and what the record list
// must render as an empty state rather than a failure.
func FromDocument(d core.Document) (Config, error) {
	raw, ok := d.Raw(ModuleName)
	if !ok {
		return Config{}, nil
	}
	c, err := UnmarshalConfig(raw)
	if err != nil {
		return Config{}, err
	}
	return c, nil
}
