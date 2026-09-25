// Package dial owns this box's uplink: the way the router itself reaches the
// internet, and a public name kept pointing at the address it has today.
//
// # What this is, and what it is not
//
// design.md §9 milestone 1 owes a `dial` module that brings the uplink up —
// PPPoE, a DHCP client, LTE, whatever the ISP wants — and holds the facts about
// it that everything downstream reads. This is two pieces of that module rather
// than the whole of it, in the way internal/link is most but not all of what
// the same milestone owes it.
//
//   - **The uplink** (Uplink), in its **static** form only: an interface, an
//     address, a default gateway. olr writes all three, and owns the default
//     route in the main table while it does. That last part is what design.md
//     §3.4 means by not squatting shared state — the main route table is
//     touched "only when a module explicitly owns that concern", which is a
//     condition rather than a ban, and this is the module that owns it.
//   - **Dynamic DNS** (Record), which is here because docs/ddns.md §2 decided
//     that the module owning the fact owns the feature, and the value published
//     is the uplink's public address.
//
// The uplink came second, and it came because the tree could not be read
// straight otherwise. internal/link/writer.go claims IPv4 ownership of every
// network member and bounds that claim with "WAN interfaces are `dial`'s"; this
// package used to answer that there was no uplink object. So the only place an
// operator could put their modem-facing NIC was a `link` network, which has
// nowhere to hold a gateway — an operator with three NICs reported that they
// could not reach a working configuration at all, and they were right.
//
// Still owed, and deliberately not invented here: the DHCP-client, PPPoE and
// LTE forms, IPv6 and prefix delegation, and multi-WAN. The first three each
// bring a backend to supervise and an address discovered rather than typed;
// Uplink is a pointer to one object precisely so that multi-WAN becomes a list
// later without changing what an already-stored document means.
//
// Two properties hold across both halves:
//
//   - **Intent is stored; the machine is read.** What address the uplink should
//     have is intent and lives in the document. What address the interface
//     *has*, and what is actually in the main route table, is read per request
//     through LinkView and the writer (§4.5). There is no cached copy of either
//     to drift — which is also how a hand-run `ip route del` shows up as drift
//     rather than as agreement.
//   - **Nothing here supervises a backend.** No unit to drive and no file to
//     render, like `link`: the uplink is programmed straight into the kernel
//     through netlink, and the DDNS publisher runs inside olrd the way
//     `gateway`'s exit prober does. design.md §3.5's test decides the second:
//     if olrd is stopped the name stops being *updated*, which is a loss of
//     freshness rather than of service.
package dial

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/netip"
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
// Two fields, and they are shaped differently on purpose: one uplink, many
// records. Records are a list of structs rather than of names — the opposite of
// internal/link's bet, and for the opposite reason: a DDNS record has eight
// fields the day it is written, so a list of names would have to become a list
// of structs immediately.
type Config struct {
	// Uplink is how this box itself reaches the internet. Nil means olr does
	// not own that, which is what every box before this ran as.
	Uplink *Uplink `json:"uplink,omitempty"`

	// Records are the public names this box keeps current.
	Records []Record `json:"records,omitempty"`
}

// Uplink is how this box itself reaches the internet.
//
// Nil means olr does not own the way out — the default route is whatever the
// distribution, a DHCP client or somebody's hand put in the main table. That is
// what every box before this ran as, and it stays a supported arrangement: the
// reference topology (docs/dns.md §1) puts olr beside the modem with no WAN
// interface of its own.
//
// Setting it is the operator saying *olr owns the way out*. From then on olr
// writes the address, brings the interface up, and replaces the default route
// in the main table — and puts all three back when olrd starts, because the
// kernel forgets them on a reboot and nothing else on the box knows them.
//
// One object rather than a list. Multi-WAN is a real thing, and it needs a
// selection policy — docs/gateway.md §2.1's ladder — plus metric and failover
// semantics that do not exist anywhere in this tree yet. A pointer to one
// object becomes a list the day those do, without the meaning of an
// already-stored document changing; internal/gateway's Exits is the shape to
// copy when it is time.
type Uplink struct {
	// Interface is the kernel name of the NIC facing the modem or the ISP.
	//
	// It is `dial`'s outright, and Validate refuses to let it also be a member
	// of a `link` network. A network is "a network this box serves"
	// (design.md §4.4) — `dhcp`, `dns`, `firewall` and later `wifi` all key off
	// one, and design.md §5.6 makes "we never serve DHCP on a WAN interface" a
	// structural exception that "follows from role rather than from
	// observation". Putting the uplink in a network would hand every one of
	// those modules a network they must special-case.
	//
	// The refusal is also what makes internal/link/writer.go's ownership claim
	// true rather than vacuous: that file strips foreign IPv4 addresses from
	// network members on the grounds that WAN interfaces live here, and until
	// this field existed they had nowhere to live.
	Interface string `json:"interface"`

	// IPv4 is the static addressing.
	//
	// Nil means olr writes no address and no route, which is deliberately a
	// legal state rather than a hole: it is the shape the DHCP-client and PPPoE
	// forms take when they land, where the address and the gateway are
	// discovered rather than typed. Today it means olr owns the interface and
	// nothing else, and the plan says so.
	IPv4 *UplinkIPv4 `json:"ipv4,omitempty"`

	// DNS are the resolvers this router itself looks names up through, in
	// preference order — usually the modem, or the ISP's.
	//
	// Written for the box whenever the uplink is static (internal/host):
	// /etc/resolv.conf, or systemd-resolved where the box resolves through it.
	// That is the ownership decision design.md §3.4 required before the file
	// could be touched, and it is made here for the reason the route table is:
	// a static uplink replaces the DHCP client that would otherwise have
	// supplied both. With no static address they are only recorded, and
	// Validate says so.
	//
	// Not olr's own resolver's upstream: dns.Upstream defaults to ModeRecurse
	// and resolves from the root. The §4.1 arrow `dial → dns (upstream
	// resolvers)` is still unbuilt.
	DNS []netip.Addr `json:"dns,omitempty"`
}

// UplinkIPv4 is the uplink's static IPv4 configuration.
type UplinkIPv4 struct {
	// Address is this box's own address on the link, with its mask —
	// 192.168.2.9/24, host bits and all.
	//
	// Deliberately **not** masked by Normalize, which is the opposite of what
	// link.NetworkIPv4.Subnet does one module away, and the two look alike enough
	// that the difference is worth stating. That field is a network and the
	// operator thinks of it as one, so masking 192.168.1.1/24 down to
	// 192.168.1.0/24 removes a typo. This field is an address; masking it would
	// quietly turn the box's own address into the network address, which
	// netlink accepts and nothing can reach.
	Address netip.Prefix `json:"address"`

	// Gateway is the next hop for the default route — the modem's address.
	//
	// This is the field that had nowhere to live. An operator who gave their
	// modem-facing NIC a static address on the Networks page found no box to
	// type this into, and a router with no default route is not a router.
	Gateway netip.Addr `json:"gateway"`
}

// HasIPv4 reports whether the uplink carries a usable static IPv4
// configuration. Both halves or neither: an address with no gateway leaves the
// box on the link and off the internet, and a gateway with no address is a next
// hop nothing can reach.
func (u *Uplink) HasIPv4() bool {
	return u != nil && u.IPv4 != nil && u.IPv4.Address.IsValid() && u.IPv4.Gateway.IsValid()
}

// Clone returns a deep copy of the uplink, so a proposal can be edited without
// disturbing the one held by the store.
func (u *Uplink) Clone() *Uplink {
	if u == nil {
		return nil
	}
	out := *u
	out.DNS = slices.Clone(u.DNS)
	if u.IPv4 != nil {
		ipv4 := *u.IPv4
		out.IPv4 = &ipv4
	}
	return &out
}

// Equal reports whether two uplinks are configured identically. Used by the
// plan, which has to tell "no change" from "the same fields, re-sent".
func (u *Uplink) Equal(other *Uplink) bool {
	switch {
	case u == nil || other == nil:
		return u == other
	case u.Interface != other.Interface:
		return false
	case !slices.Equal(u.DNS, other.DNS):
		return false
	case (u.IPv4 == nil) != (other.IPv4 == nil):
		return false
	case u.IPv4 == nil:
		return true
	}
	return *u.IPv4 == *other.IPv4
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

// SetUplink replaces the uplink.
func (c *Config) SetUplink(u Uplink) {
	c.Uplink = &u
	c.Normalize()
}

// RemoveUplink drops the uplink, reporting whether there was one.
//
// Note what it does not do, and it is the same asymmetry RemoveRecord has: it
// does not take the address off the interface, and it does not delete the
// default route. Both stay exactly as olr last programmed them, so the box
// keeps reaching the internet; what stops is olr *owning* them — putting them
// back after a reboot, and replacing the route when the gateway changes.
//
// The alternative is worse in a way that has no recovery. olr never recorded
// what the main table held before it claimed it — link.Applier.Restore refuses
// to save state for the same reason — so a teardown could not put the previous
// route back, only leave the box with none. The usual reason to remove an
// uplink is to hand the interface to something else (the distribution, or the
// PPPoE form when it lands), and taking the route away first is exactly the
// wrong opening move. print.go and the plan say all this out loud, because
// "removed" would otherwise read as "this box is offline now".
func (c *Config) RemoveUplink() bool {
	if c.Uplink == nil {
		return false
	}
	c.Uplink = nil
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
//
// It is what `olr status` and startDial read to decide whether this module has
// been asked for anything, so the uplink counts: a box with an uplink and no
// records is very much configured, and skipping it at startup would leave the
// default route unrestored after every reboot.
func (c Config) Empty() bool { return c.Uplink == nil && len(c.Records) == 0 }

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
	normalizeUplink(c.Uplink)
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

// normalizeUplink puts the uplink in canonical form.
//
// Note the two things it does *not* do, both load-bearing:
//
// It does not mask Address. See UplinkIPv4.Address — that is an address, and
// masking it would turn the box's own address into the network address.
//
// It does not sort DNS. Every other list in this tree is sorted so that a
// reordered array cannot read as a change nobody made, and here the order *is*
// the change: resolvers are tried in the order given, so sorting them would
// silently re-rank the operator's preference. Duplicates still go, because a
// resolver listed twice is a typo in every case.
func normalizeUplink(u *Uplink) {
	if u == nil {
		return
	}
	u.Interface = strings.TrimSpace(u.Interface)

	if u.IPv4 != nil {
		// An unparseable or absent address leaves the block meaning nothing, and
		// keeping a half-filled struct in the document would make "no static
		// addressing" and "a broken static addressing" look the same to every
		// reader downstream. Validate complains about the half-filled form
		// first, so this only ever collapses the wholly empty one.
		if !u.IPv4.Address.IsValid() && !u.IPv4.Gateway.IsValid() {
			u.IPv4 = nil
		} else {
			u.IPv4.Gateway = u.IPv4.Gateway.Unmap()
		}
	}

	seen := make(map[netip.Addr]bool, len(u.DNS))
	out := make([]netip.Addr, 0, len(u.DNS))
	for _, addr := range u.DNS {
		addr = addr.Unmap().WithZone("")
		if !addr.IsValid() || seen[addr] {
			continue
		}
		seen[addr] = true
		out = append(out, addr)
	}
	if len(out) == 0 {
		u.DNS = nil
	} else {
		u.DNS = out
	}
}

// Clone returns a deep copy, so a proposal can be edited without disturbing the
// one held by the store.
func (c Config) Clone() Config {
	return Config{Uplink: c.Uplink.Clone(), Records: slices.Clone(c.Records)}
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
