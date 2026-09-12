package dial

import (
	"errors"
	"fmt"
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

// Validate checks the records against the vocabulary and against the box.
//
// `links` may be nil, in which case the rules that need an interface list are
// skipped. That keeps the rest testable without a network the way design.md
// §5.3.1 asks, and every rule it skips is one this module can only warn about
// anyway — an uplink that is down or has no address yet is a normal state for a
// line that has not come up, not a config that cannot be stored.
func Validate(c Config, links LinkView) Result {
	var r Result

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
