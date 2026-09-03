package dnsrelay

import (
	"net/netip"
	"sort"
	"strings"
)

// Deciding, per client, whether we answer a query ourselves.
//
// This is the second of the three things docs/dns.md §4 says owning :53 is for,
// and it lives here rather than in unbound's views for a reason that only
// becomes visible once the relay exists: unbound selects a view by the client's
// netblock, and every query reaching unbound now comes from 127.0.0.1. All
// views would collapse into one. The relay is the only thing left that still
// knows who asked.

// PolicySet is the compiled form of the policy files, ready for the hot path.
//
// Built once per reload and then read-only, so the query path takes no lock and
// allocates nothing. It is swapped atomically; a query in flight finishes
// against the set it started with, which is correct — a SIGHUP is not a promise
// that in-flight queries are re-decided.
type PolicySet struct {
	// byPrefix is every claimed network, most specific first. First match wins,
	// which given the ordering means the most specific match wins: a /32 for
	// one tablet beats a /24 for the house.
	byPrefix []prefixPolicy

	// fallback governs clients no prefix claims. Nil means no policy applies
	// and everything is relayed.
	fallback *compiledPolicy
}

type prefixPolicy struct {
	prefix netip.Prefix
	policy *compiledPolicy
}

type compiledPolicy struct {
	name     string
	response string

	// block and allow are suffix-matched. Held as plain sorted slices rather
	// than a trie: a house blocklist is tens to thousands of entries, and the
	// matcher below walks a name's parent labels — at most 127 of them, in
	// practice five — so the lookup is a handful of map probes either way.
	block map[string]bool
	allow map[string]bool
}

// Compile turns the loaded policy files into a decision structure.
func Compile(policies []Policy) *PolicySet {
	set := &PolicySet{}

	for i := range policies {
		p := policies[i]
		compiled := &compiledPolicy{
			name:     p.Name,
			response: p.Response,
			block:    nameSet(p.Block),
			allow:    nameSet(p.Allow),
		}
		if compiled.response == "" {
			compiled.response = RespondNXDOMAIN
		}

		if len(p.Clients) == 0 {
			// The default policy. Two of them would be a configuration olr
			// refuses to write, so the last one wins here rather than growing a
			// second error path in the data plane.
			set.fallback = compiled
			continue
		}
		for _, c := range p.Clients {
			set.byPrefix = append(set.byPrefix, prefixPolicy{prefix: c.Masked(), policy: compiled})
		}
	}

	// Most specific first. Sorting once at compile time is what lets the hot
	// path be a linear scan that stops at the first hit.
	sort.SliceStable(set.byPrefix, func(i, j int) bool {
		return set.byPrefix[i].prefix.Bits() > set.byPrefix[j].prefix.Bits()
	})

	return set
}

func nameSet(names []string) map[string]bool {
	if len(names) == 0 {
		return nil
	}
	out := make(map[string]bool, len(names))
	for _, n := range names {
		out[n] = true
	}
	return out
}

// Decision is what a policy said about one query.
type Decision struct {
	// Blocked reports that we answer this ourselves.
	Blocked bool

	// Policy names the rule that decided, which is what turns the query log
	// from "this was blocked" into "this was blocked by the kids policy".
	Policy string

	// Response is how the refusal is spelled: nxdomain or zero.
	Response string
}

// Decide answers whether a client may look up a name.
//
// Nil receiver is legal and means "no policy configured", which is the ordinary
// state of a box that only wants the query log.
func (s *PolicySet) Decide(client netip.Addr, name string) Decision {
	if s == nil {
		return Decision{}
	}
	p := s.policyFor(client)
	if p == nil {
		return Decision{}
	}

	// Allow is checked first and wins outright, so "block social media except
	// the one site the school uses" is expressible without inverting the list.
	if matches(p.allow, name) {
		return Decision{Policy: p.name}
	}
	if matches(p.block, name) {
		return Decision{Blocked: true, Policy: p.name, Response: p.response}
	}
	return Decision{Policy: p.name}
}

// PolicyFor names the policy governing a client, for callers that want to
// report it without deciding anything.
func (s *PolicySet) PolicyFor(client netip.Addr) string {
	if s == nil {
		return ""
	}
	if p := s.policyFor(client); p != nil {
		return p.name
	}
	return ""
}

func (s *PolicySet) policyFor(client netip.Addr) *compiledPolicy {
	// Unmap first: a v4 client arriving on a dual-stack socket presents as
	// ::ffff:192.168.1.5, which no IPv4 prefix contains. Without this, every
	// policy silently stops applying the moment the relay binds a v6 address.
	client = client.Unmap()
	for _, pp := range s.byPrefix {
		if pp.prefix.Contains(client) {
			return pp.policy
		}
	}
	return s.fallback
}

// matches reports whether a name or any of its parents is in the set.
//
// A pattern covers the name and everything under it: "example.com" matches
// example.com and www.example.com alike. That is the reading an operator means
// when they block a site, and exact-only matching would be a support question
// on the first day — the entry would appear to do nothing, because nobody
// resolves the bare apex.
func matches(set map[string]bool, name string) bool {
	if len(set) == 0 || name == "" {
		return false
	}
	for {
		if set[name] {
			return true
		}
		dot := strings.IndexByte(name, '.')
		if dot < 0 {
			return false
		}
		name = name[dot+1:]
	}
}
