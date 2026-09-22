package dial

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"strings"

	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/dial/provider"
)

// Problem is one validation finding, addressed by a JSON-ish path so a UI can
// attach it to the field that caused it. Mirrors every other module's shape,
// and is converted to core.Problem at the view boundary.
type Problem struct {
	Path    string
	Message string
}

func (p Problem) String() string {
	if p.Path == "" {
		return p.Message
	}
	return p.Path + ": " + p.Message
}

// Result separates the fatal from the merely suspect.
type Result struct {
	Errors   []Problem
	Warnings []Problem
}

func (r *Result) errorf(path, format string, args ...any) {
	r.Errors = append(r.Errors, Problem{Path: path, Message: fmt.Sprintf(format, args...)})
}

func (r *Result) warnf(path, format string, args ...any) {
	r.Warnings = append(r.Warnings, Problem{Path: path, Message: fmt.Sprintf(format, args...)})
}

// OK reports whether the config can be stored.
func (r Result) OK() bool { return len(r.Errors) == 0 }

// Err collapses the errors into one, or nil.
func (r Result) Err() error {
	if r.OK() {
		return nil
	}
	msgs := make([]error, len(r.Errors))
	for i, p := range r.Errors {
		msgs[i] = errors.New(p.String())
	}
	return fmt.Errorf("invalid dial configuration:\n  %w", errors.Join(msgs...))
}

// Validate checks the uplink and the records against the vocabulary and
// against the box.
//
// `links` may be nil, in which case the rules that need an interface list are
// skipped. That keeps the rest testable without a network the way design.md
// §5.3.1 asks, and every rule it skips is one this module can only warn about
// anyway — an uplink that is down or has no address yet is a normal state for a
// line that has not come up, not a config that cannot be stored.
//
// The one exception is the network-overlap refusal, which *is* an error and
// does need `links`. It is still skipped without one, and that is the right
// trade: a box whose interface list cannot be read is not a box to refuse
// configuration on, and the overlap is re-checked on every subsequent save.
func Validate(c Config, links LinkView) Result {
	var r Result

	validateUplink(&r, c.Uplink, links)

	seen := map[string]int{}
	for i, rec := range c.Records {
		path := fmt.Sprintf("records[%d]", i)

		if first, dup := seen[rec.Name]; dup {
			// Two entries for one name would each publish over the other, and
			// each would then see the other's write as an address change — so
			// the pair would call the provider on every check forever.
			r.errorf(path, "%q is already configured at records[%d]; "+
				"one name is kept current by one record", rec.Name, first)
			continue
		}
		seen[rec.Name] = i

		validateName(&r, path, rec)
		validateProvider(&r, path, rec)
		validateSource(&r, path, rec, links)
		validateTiming(&r, path, rec)
	}

	return r
}

// UplinkPath is the field path every uplink finding is reported against, so a
// UI can attach one to the section rather than to a record row.
const UplinkPath = "uplink"

// validateUplink checks the box's own way out.
//
// The shape of the rules here is worth stating, because it is not the same as
// the records': one refusal is about *ownership* rather than about the values,
// and the two most useful findings are warnings that describe a configuration
// which is entirely coherent and still will not carry traffic. That is the same
// distinction internal/gateway/nat/validate.go draws, for the same reason — the
// operator's question is not "is this well formed" but "will this work".
func validateUplink(r *Result, u *Uplink, links LinkView) {
	if u == nil {
		return
	}

	if u.Interface == "" {
		r.errorf(UplinkPath+".interface",
			"an uplink needs the interface facing your modem or your ISP")
	}
	validateUplinkIPv4(r, u)
	validateUplinkDNS(r, u)

	if links == nil || u.Interface == "" {
		return
	}
	checkUplinkInterface(r, u, links)
}

// validateUplinkIPv4 checks the static addressing.
func validateUplinkIPv4(r *Result, u *Uplink) {
	if u.IPv4 == nil {
		// Legal, and the plan says what it means: olr owns the interface and
		// writes neither an address nor a route. It is the state the
		// DHCP-client and PPPoE forms arrive in.
		return
	}
	addr, gw := u.IPv4.Address, u.IPv4.Gateway

	switch {
	case !addr.IsValid():
		r.errorf(UplinkPath+".ipv4.address",
			"give this box's own address on the uplink, with its mask — 192.168.2.9/24")
	case !addr.Addr().Is4():
		// §3 of the plan this was built from: IPv4 only, matching
		// link.GroupIPv4's precedent. A v6 uplink needs a v6 write path, and
		// accepting the field before that exists would be a promise every
		// generated surface makes and the writer cannot keep.
		r.errorf(UplinkPath+".ipv4.address",
			"%s is IPv6; olr does not configure an IPv6 uplink yet", addr)
	case addr.Addr().IsUnspecified(), addr.Addr().IsLoopback(), addr.Addr().IsMulticast():
		r.errorf(UplinkPath+".ipv4.address", "%s cannot be an interface's address", addr)
	case addr.Bits() < 31 && addr.Addr() == addr.Masked().Addr():
		// The host-bits check. Normalize deliberately does not mask this field
		// — see UplinkIPv4.Address — so the typo has to be caught rather than
		// quietly corrected, and the correction would be the wrong one anyway:
		// 192.168.2.0/24 is the network, not an address anything can reach.
		// /31 and /32 are exempt because they have no network address.
		r.errorf(UplinkPath+".ipv4.address",
			"%s is the network itself rather than this box's address on it; "+
				"give the address your ISP or modem assigned, such as %s",
			addr, exampleHost(addr))
	}

	if !gw.IsValid() {
		r.errorf(UplinkPath+".ipv4.gateway",
			"give the gateway to send traffic to — the modem's address on this link. "+
				"Without it this box has an address and no way out")
		return
	}
	if !gw.Is4() {
		r.errorf(UplinkPath+".ipv4.gateway",
			"%s is IPv6; olr does not configure an IPv6 uplink yet", gw)
		return
	}

	if addr.IsValid() && addr.Addr().Is4() {
		switch {
		case gw == addr.Addr():
			r.errorf(UplinkPath+".ipv4.gateway",
				"%s is this box's own address; the gateway is the device on the other "+
					"end of the link, usually your modem", gw)
		case !addr.Contains(gw):
			// An off-link next hop needs an explicit route to itself first, and
			// olr writes no such route. The kernel would refuse the route with
			// ENETUNREACH, which arrives as an opaque failure halfway through an
			// apply rather than as a sentence about the two fields that disagree.
			r.errorf(UplinkPath+".ipv4.gateway",
				"%s is not inside %s, so this box has no way to reach it. "+
					"The gateway is on the same link as the address — check the mask",
				gw, addr.Masked())
		}
	}

	if addr.IsValid() && addr.Addr().Is4() && addr.Addr().IsPrivate() {
		// Double NAT, said out loud and never refused — internal/gateway/nat's
		// warnUnreachableUplink makes the same call about the same fact, and for
		// the same reason: a router chained behind another router is ugly and
		// works, so refusing would block a configuration that carries traffic
		// today. What it costs is inbound: nothing on the internet can open a
		// connection to this box without the device in front forwarding it.
		r.warnf(UplinkPath+".ipv4.address",
			"%s is a private address, so this router is behind another one. "+
				"That works for reaching the internet, and it means nothing outside "+
				"can open a connection to this box unless the device in front "+
				"forwards it", addr.Addr())
	}
}

// validateUplinkDNS checks the resolvers, and says when they will not be used.
//
// They are this router's own resolvers whenever the uplink is static: olr
// writes them where the box looks names up (internal/host), because a static
// uplink replaces the DHCP client that would otherwise have supplied them —
// and on the box that forced this, the one that had been supplying them wrote
// an empty file, so the router reached the internet and resolved nothing.
//
// With no static address olr writes nothing at all to the interface, so
// whatever the distribution runs there still provides the resolvers, and these
// are only recorded. That is the one case worth a warning.
func validateUplinkDNS(r *Result, u *Uplink) {
	if len(u.DNS) == 0 {
		return
	}
	for i, addr := range u.DNS {
		if !addr.IsValid() {
			r.errorf(fmt.Sprintf("%s.dns[%d]", UplinkPath, i), "not an IP address")
		}
	}
	if !u.HasIPv4() {
		r.warnf(UplinkPath+".dns",
			"these are recorded and not used: with no static address olr does not configure "+
				"%s, so whatever the distribution runs on it still chooses this router's resolvers",
			u.Interface)
	}
}

// checkUplinkInterface holds the rules that need the box.
func checkUplinkInterface(r *Result, u *Uplink, links LinkView) {
	info, err := links.Interface(u.Interface)
	if err != nil {
		r.errorf(UplinkPath+".interface", "this machine has no interface named %q", u.Interface)
		return
	}
	if !info.Adopted {
		// The adopt-only rule (design.md §3.4, §7), enforced here rather than in
		// `link` for the reason internal/link/validate.go gives: the complaint
		// belongs against the field that named the interface.
		r.errorf(UplinkPath+".interface", "%q has not been handed to olr; "+
			"run `olr adopt %s` first", u.Interface, u.Interface)
		return
	}

	groups, err := links.Groups()
	if err == nil {
		if g, taken := groupFor(groups, u.Interface); taken {
			// The refusal that guides an existing box's migration. Somebody who
			// gave their modem-facing NIC a static address before this object
			// existed did it on the Networks page, because that was the only
			// place in olr that would take one, and docs/install.md told them to.
			// Two owners for one interface's addressing is the state
			// internal/link/writer.go's ownership claim cannot survive, so it is
			// refused here rather than resolved.
			r.errorf(UplinkPath+".interface",
				"%s carries the network %q, and an uplink and a network cannot both own "+
					"one interface's addressing. A network is something this box *serves* "+
					"— it gets DHCP, DNS and a router address — and the way out is not. "+
					"Remove the network first (`olr net rm %s`), then set the uplink",
				u.Interface, g.Name, g.Name)
		}
	}

	if !info.Up {
		r.warnf(UplinkPath+".interface",
			"%q is down; olr brings it up when this is applied", u.Interface)
	}
}

// unroutedNetworksNote is the trap this object is most likely to leave behind,
// as a sentence — or "" when there is nothing to say.
//
// Setting the uplink gets *this box* onto the internet. It does not get the
// networks behind it there, and the reason is not visible from anywhere an
// operator is looking: source NAT hangs off a `gateway` exit, not off the
// route, so a LAN packet leaving through the default route leaves with its LAN
// source address still on it and the modem has no route back. The symptom is
// that the box reaches the internet and nothing else does, which reads like DNS
// or a firewall and is neither.
//
// # Why it is a plan note rather than a validation warning
//
// Because it cannot be switched off by doing the work. The check that would
// make it disappear — "is there a `gateway` exit covering each network" — needs
// `gateway`'s config, and design.md §4.1's arrow points `dial → gateway`;
// inverting it to silence a note would buy a cycle for a cosmetic gain. So
// instead of nagging on every status read forever, it is attached to the plan
// of a change, where dial already puts the consequence a machine's state does
// not show, and where the operator is looking at what they just asked for.
func unroutedNetworksNote(u *Uplink, links LinkView) string {
	if links == nil || u == nil || !u.HasIPv4() {
		return ""
	}
	groups, err := links.Groups()
	if err != nil || len(groups) == 0 {
		return ""
	}
	names := make([]string, 0, len(groups))
	for _, g := range groups {
		names = append(names, g.Name)
	}
	subject, object := "the network "+names[0], "it"
	if len(names) > 1 {
		subject, object = "the networks "+strings.Join(names, ", "), "them"
	}
	return fmt.Sprintf("this gets the router itself onto the internet; %s will not follow "+
		"until an exit sends %s there. Traffic leaving through the default route is not "+
		"translated, so replies have nowhere to come back to. Add one with "+
		"`olr gateway add exit internet --next-hop %s --dev %s`, then "+
		"`olr gateway set via <network> internet`",
		subject, object, u.IPv4.Gateway, u.Interface)
}

// exampleHost suggests a host address inside a prefix the operator gave as a
// network, so the refusal shows the shape of the right answer.
func exampleHost(p netip.Prefix) netip.Prefix {
	host, ok := core.FirstHost(p)
	if !ok {
		return p
	}
	return netip.PrefixFrom(host, p.Bits())
}

// validateName checks the thing being published.
func validateName(r *Result, path string, rec Record) {
	name := rec.Name
	switch {
	case name == "":
		r.errorf(path+".name", "a record needs the name it keeps current, such as home.example.net")
		return
	case !strings.Contains(name, "."):
		r.errorf(path+".name", "%q is a single label; a public name has a domain after it, "+
			"such as home.example.net", name)
		return
	case strings.ContainsAny(name, " \t/"):
		r.errorf(path+".name", "%q is not a DNS name", name)
		return
	}

	if !isASCII(name) {
		// Cloudflare's API expects punycode, and the other two are no better
		// about it. Refusing here beats letting the provider answer "no such
		// record" about a name that looks correct on screen.
		r.errorf(path+".name", "%q has non-ASCII characters; give it in its punycode form "+
			"(the xn-- spelling), which is what the providers' APIs expect", name)
		return
	}

	if rec.ZoneOrDerived() == "" {
		// The public suffix list could not split the name and the operator did
		// not say. Naming the field is the whole value of this check: without
		// it the first failure is "no zone named home.example.internal" from a
		// provider, which reads like a credential problem.
		r.errorf(path+".zone", "olr cannot tell which registered domain %q sits in; "+
			"set the zone explicitly", rec.Name)
		return
	}
	if zone := rec.ZoneOrDerived(); zone != rec.Name && !strings.HasSuffix(rec.Name, "."+zone) {
		r.errorf(path+".zone", "%q is not inside the zone %q", rec.Name, zone)
	}
}

// validateProvider checks the provider and the credential shape it needs.
//
// The credential rules differ per provider and are stated here rather than
// discovered at the endpoint, because "401" three hours after a config was
// saved is a worse place to learn that Alibaba needs a key ID than the moment
// the field was left empty.
func validateProvider(r *Result, path string, rec Record) {
	if rec.Provider == "" {
		r.errorf(path+".provider", "a record needs a provider (one of %s)",
			strings.Join(provider.Names(), ", "))
		return
	}
	if !rec.Provider.Known() {
		// docs/ddns.md §4.4: upstream's dispatch ends in a default that turns a
		// typo into Alibaba DNS. There is no default here and there is no
		// silent one either.
		r.errorf(path+".provider", "no provider named %q (have %s); "+
			"olr refuses a name it does not implement rather than guessing one",
			rec.Provider, strings.Join(provider.Names(), ", "))
		return
	}

	switch string(rec.Provider) {
	case provider.NameCloudflare:
		if rec.Token == "" {
			r.errorf(path+".provider_token", "cloudflare needs an API token")
		}
		if rec.KeyID != "" {
			r.errorf(path+".provider_key_id", "cloudflare authenticates with a token alone; "+
				"there is no key ID to set")
		}
		if rec.CallbackURL != "" {
			r.errorf(path+".callback_url", "a callback URL only applies to the callback provider")
		}
	case provider.NameAlidns, provider.NameTencentCloud:
		if rec.KeyID == "" {
			r.errorf(path+".provider_key_id", "%s needs the key ID as well as the secret (%s)",
				rec.Provider, keyIDNames[string(rec.Provider)])
		}
		if rec.Token == "" {
			r.errorf(path+".provider_token", "%s needs the secret that goes with the key ID (%s)",
				rec.Provider, tokenNames[string(rec.Provider)])
		}
		if rec.CallbackURL != "" {
			r.errorf(path+".callback_url", "a callback URL only applies to the callback provider")
		}
	case provider.NameCallback:
		if rec.CallbackURL == "" {
			r.errorf(path+".callback_url", "callback needs the URL to request, "+
				"with #{ip} where the address goes")
			break
		}
		if problem := checkHTTPURL(rec.CallbackURL); problem != "" {
			r.errorf(path+".callback_url", "%s", problem)
			break
		}
		if !strings.Contains(rec.CallbackURL, "#{ip}") && rec.Token == "" {
			// A callback that mentions the address nowhere publishes nothing.
			// It is legal — some endpoints read the source address of the
			// request itself — so this is a warning, but it is the mistake
			// somebody makes on their first attempt.
			r.warnf(path+".callback_url", "neither the URL nor the request body mentions #{ip}, "+
				"so this endpoint is told the name changed and never told the address; "+
				"that is right only if it reads the address off the connection itself")
		}
		if rec.KeyID != "" {
			r.errorf(path+".provider_key_id", "callback has no key ID; "+
				"put whatever the endpoint needs in the URL or the request body")
		}
	}
}

// The two halves of a key pair, in each vendor's own words, so an operator can
// match the message to the console they are looking at.
var (
	keyIDNames = map[string]string{
		provider.NameAlidns:       "AccessKey ID",
		provider.NameTencentCloud: "SecretId",
	}
	tokenNames = map[string]string{
		provider.NameAlidns:       "AccessKey Secret",
		provider.NameTencentCloud: "SecretKey",
	}
)

// validateSource checks where the address comes from.
func validateSource(r *Result, path string, rec Record, links LinkView) {
	if !rec.Source.Valid() {
		if rec.Source == "" {
			// The one enum in this tree with no default, and the message says
			// why: the two answers differ in whether this box talks to a
			// stranger every five minutes, and choosing on the operator's
			// behalf is what design.md §5.6 forbids.
			r.errorf(path+".source", "say where the address comes from: "+
				"%q reads it off the uplink, which is right when olr terminates the WAN, "+
				"or %q asks an endpoint what address the internet sees, which is right "+
				"when olr is behind a modem. olr will not choose for you",
				SourceInterface, SourceReflector)
			return
		}
		r.errorf(path+".source", "no source named %q (have %s)", rec.Source, joinSources())
		return
	}

	switch rec.Source {
	case SourceInterface:
		if rec.ReflectorURL != "" {
			r.errorf(path+".reflector_url", "the source is %q, so the address is read off %s; "+
				"remove the reflector or set the source to %q",
				SourceInterface, orInterface(rec.Interface), SourceReflector)
		}
		if rec.Interface == "" {
			r.errorf(path+".interface", "the source is %q, so it needs the interface to read it off",
				SourceInterface)
			return
		}
	case SourceReflector:
		if rec.ReflectorURL == "" {
			r.errorf(path+".reflector_url", "the source is %q, so it needs the endpoint to ask. "+
				"olr ships no default: one would mean every olr installation reporting its "+
				"address to the same third party on a timer", SourceReflector)
		} else if problem := checkHTTPURL(rec.ReflectorURL); problem != "" {
			r.errorf(path+".reflector_url", "%s", problem)
		} else if strings.HasPrefix(strings.ToLower(rec.ReflectorURL), "http://") {
			r.warnf(path+".reflector_url", "this endpoint is plain HTTP, so anything between "+
				"here and it can change the address olr publishes")
		}
		if rec.Interface == "" {
			// Not an error: a box with one way out gets the right answer
			// without binding. It is the multi-WAN box that publishes whichever
			// answer it happened to get (docs/ddns.md §3.3).
			return
		}
	}

	checkInterface(r, path, rec, links)
}

// checkInterface holds the rules that need the box.
func checkInterface(r *Result, path string, rec Record, links LinkView) {
	if links == nil || rec.Interface == "" {
		return
	}

	info, err := links.Interface(rec.Interface)
	if err != nil {
		r.errorf(path+".interface", "this machine has no interface named %q", rec.Interface)
		return
	}
	if !info.Adopted {
		// The adopt-only rule (design.md §3.4, §7), enforced here rather than in
		// `link` for the reason internal/link/validate.go gives: the complaint
		// belongs against the field that named the interface.
		r.errorf(path+".interface", "%q has not been handed to olr; "+
			"run `olr adopt %s` first", rec.Interface, rec.Interface)
		return
	}
	if !info.Up {
		r.warnf(path+".interface", "%q is down, so nothing will be published until it comes up",
			rec.Interface)
		return
	}
	if rec.Source != SourceInterface {
		return
	}

	addr, ok := info.PublicIPv4()
	switch {
	case !ok:
		r.warnf(path+".interface", "%q has no IPv4 address yet, so there is nothing to publish",
			rec.Interface)
	case addr.IsPrivate():
		// The reference topology's trap, said out loud (docs/ddns.md §3.1): olr
		// behind the modem reads an RFC1918 address off its uplink, and
		// publishing it produces a name that resolves to something nobody
		// outside the house can reach. A warning rather than an error, because
		// an operator whose olr *is* the WAN router and who is deliberately
		// publishing a private address into a split-horizon zone is doing
		// something legitimate — and because design.md §5.6 forbids us
		// switching them to the reflector form on their behalf.
		r.warnf(path+".interface", "%q currently has the private address %s. "+
			"Publishing it gives a name nobody outside this network can reach — "+
			"if olr is behind a modem, use the %q source instead",
			rec.Interface, addr, SourceReflector)
	}
}

// validateTiming checks the schedule and the TTL.
func validateTiming(r *Result, path string, rec Record) {
	switch {
	case rec.Interval < 0:
		r.errorf(path+".interval", "an interval cannot be negative")
	case rec.Interval > 0 && rec.Interval.Duration() < MinInterval:
		r.errorf(path+".interval", "%s is faster than olr will check (the floor is %s). "+
			"The reflector form sends a request to somebody else's endpoint on this timer, "+
			"and the free ones are free on the understanding that nobody polls them",
			rec.Interval, Duration(MinInterval))
	}

	if rec.TTL < 0 {
		r.errorf(path+".ttl", "a TTL cannot be negative")
	}
}

// checkHTTPURL returns why s cannot be a URL we would request, or "".
func checkHTTPURL(s string) string {
	u, err := url.Parse(s)
	if err != nil {
		return fmt.Sprintf("%q is not a URL: %v", s, err)
	}
	switch {
	case u.Scheme != "http" && u.Scheme != "https":
		return fmt.Sprintf("%q is not an http:// or https:// URL", s)
	case u.Host == "":
		return fmt.Sprintf("%q has no host", s)
	}
	return ""
}

func isASCII(s string) bool {
	for i := range len(s) {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

func joinSources() string {
	out := make([]string, 0, len(Sources()))
	for _, s := range Sources() {
		out = append(out, string(s))
	}
	return strings.Join(out, ", ")
}

func orInterface(name string) string {
	if name == "" {
		return "an interface"
	}
	return name
}

// problems converts this module's findings into core's wire shape, so that
// every module reports a bad field the same way.
func problems(in []Problem) []core.Problem {
	if len(in) == 0 {
		return nil
	}
	out := make([]core.Problem, 0, len(in))
	for _, p := range in {
		out = append(out, core.Problem{Path: p.Path, Message: p.Message})
	}
	return out
}
