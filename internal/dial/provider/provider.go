// Package provider publishes one address into one DNS provider's zone.
//
// # Ported from ddns-go, not imported
//
// Every file here beside this one carries the header of the upstream file it
// came from, its commit, and the MIT notice; LICENSE.ddns-go holds the licence
// text in full. That is the first third-party copyright header in this
// repository, so docs/ddns.md §4.4 asks that it be introduced deliberately
// rather than appearing one day in a diff.
//
// What came across is the API shape: endpoints, request and response structs,
// the auth headers, the signing algorithms, the zone lookup. What did not, and
// why (docs/ddns.md §4.2):
//
//   - **The driver.** Upstream reports a failed update by setting a status
//     field on a domain struct and printing the reason to the standard logger.
//     The empty status is indistinguishable from "nothing happened", which
//     makes the one failure that matters here — a record quietly going stale —
//     invisible to the caller. Update returns an error, and that is the whole
//     reason this package exists rather than an import.
//   - **config.DnsConfig.** Upstream's providers read their credentials, their
//     TTL and their HTTP client out of the program's config struct. Here they
//     take a Record and a client, so a test can drive one against httptest
//     without constructing a program.
//   - **The package-global IP cache.** Whether an address changed is the
//     publisher's question, and it holds the answer per record (see
//     ../publisher.go). A second cache here would be a second opinion.
//   - **os/exec.** Upstream's config can shell out for the address. design.md
//     §3.6 forbids olrd starting subprocesses, and olrd.service's sandbox is
//     written on the assumption that it never does.
//   - **The `default: &Alidns{}` fallthrough.** Upstream's dispatch turns a
//     typo'd provider name into Alibaba DNS. For returns an error instead, and
//     the validator refuses the name long before this (docs/ddns.md §4.4).
//
// # One record, one answer
//
// Update creates the record if the provider does not have it and changes it if
// it does. If the provider holds *more than one* record of that name and type,
// it refuses: an operator with two A records on a name has a round-robin, and
// the two things this code could do instead — point both at one address, or
// update one and leave the other answering the old one — are both silent
// damage to something we did not put there. docs/ddns.md §8 says this is not a
// DNS control panel, and this is where that line falls.
package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
)

// Provider publishes one record.
//
// One method, returning an error, which is the point (docs/ddns.md §4.2).
type Provider interface {
	// Update makes rec.Name resolve to rec.Value, creating the record if the
	// provider does not have it.
	//
	// It is called only when the address changed (../publisher.go), so it does
	// not have to be cheap — but it is still expected to be idempotent: a
	// record that already holds the value is a success with no request made.
	Update(ctx context.Context, rec Record) error
}

// Record is everything a provider needs to publish one name.
//
// Flat, and a value rather than a pointer, because every provider reads it and
// none of them may keep it: the credential in it is the operator's and the
// shortest life it can have is one request.
type Record struct {
	// Name is the fully-qualified name being published — "home.example.net".
	Name string

	// Zone is the registered domain Name sits inside — "example.net". Every
	// provider here addresses a record by zone plus the labels below it, and
	// none of them will work that out from Name on its own.
	Zone string

	// Type is the record type. "A" is the only value v1 produces
	// (docs/ddns.md §9 #2 has not decided what AAAA means on a router yet), and
	// it is a field rather than a constant so that the providers do not have to
	// change when it does.
	Type string

	// Value is the address to publish.
	Value string

	// TTL is the record's time to live in seconds. Zero means the provider's
	// own default, which differs between them and is not ours to unify.
	TTL int

	// KeyID is the non-secret half of a two-part credential: an AccessKeyId at
	// Alibaba Cloud, a SecretId at Tencent Cloud. Cloudflare and callback do
	// not use it.
	KeyID string

	// Token is the secret half: a Cloudflare API token, an AccessKeySecret, a
	// SecretKey. For callback it is the request body template, which is
	// optional and is the usual place a callback's own credential ends up.
	Token string

	// URL is the callback provider's request URL template, and is unused by
	// everything else. It frequently contains a credential — DuckDNS and
	// several of the DynDNS v2 endpoints put the token in the query string —
	// which is why the module redacts it alongside Token.
	URL string
}

// Subdomain returns the labels of Name below Zone, or "@" when Name is the
// zone itself.
//
// "@" is the spelling Alibaba and Tencent both expect for the apex. Cloudflare
// wants the full name and does not call this.
func (r Record) Subdomain() string {
	name := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(r.Name)), ".")
	zone := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(r.Zone)), ".")
	switch {
	case name == zone, zone == "":
		return "@"
	case strings.HasSuffix(name, "."+zone):
		return strings.TrimSuffix(name, "."+zone)
	default:
		// Not inside the zone. The validator refuses this before it gets here;
		// returning the name whole keeps the provider's error about the record
		// rather than about an empty string.
		return name
	}
}

// Names lists the providers this build can publish through, sorted.
//
// One source for the schema's enum, the CLI's completion and the validator, so
// a name offered by one cannot be refused by another. The list grows by
// request rather than by completeness (docs/ddns.md §4.4).
func Names() []string {
	return []string{NameAlidns, NameCallback, NameCloudflare, NameTencentCloud}
}

// The provider names, as they are spelled in the config document.
const (
	NameCloudflare   = "cloudflare"
	NameAlidns       = "alidns"
	NameTencentCloud = "tencentcloud"
	NameCallback     = "callback"
)

// Known reports whether name is one this build implements.
func Known(name string) bool { return slices.Contains(Names(), name) }

// For returns the implementation for a provider name.
//
// An unknown name is an error and there is no default. Upstream's dispatch ends
// in `default: dnsSelected = &Alidns{}`, so a typo there silently publishes to
// Alibaba DNS — with a credential that will not authenticate, which at least
// fails, and against a zone that might (docs/ddns.md §4.4).
//
// client may be nil, in which case http.DefaultClient is used. Passing one is
// how the module binds the request to an uplink and how the tests point it at
// an httptest server.
func For(name string, client *http.Client) (Provider, error) {
	if client == nil {
		client = http.DefaultClient
	}
	switch name {
	case NameCloudflare:
		return cloudflare{client: client}, nil
	case NameAlidns:
		return alidns{client: client}, nil
	case NameTencentCloud:
		return tencentCloud{client: client}, nil
	case NameCallback:
		return callback{client: client}, nil
	default:
		return nil, fmt.Errorf("no provider named %q (have %s)",
			name, strings.Join(Names(), ", "))
	}
}

// ErrTooManyRecords is returned when the provider holds more than one record of
// the name and type being published. See the package comment.
type ErrTooManyRecords struct {
	Name  string
	Type  string
	Count int
}

func (e ErrTooManyRecords) Error() string {
	return fmt.Sprintf("%s has %d %s records at this provider; "+
		"olr publishes one address to one record and will not choose between them "+
		"or collapse them into one", e.Name, e.Count, e.Type)
}

// --- the shared request path ------------------------------------------------

// maxBody is how much of a provider's reply is read.
//
// A provider answering with something enormous is a provider having a bad day,
// and the useful part of every response here is the first few hundred bytes.
// Upstream's limit is a megabyte; this is smaller for the same reason it exists
// at all — the body reaches an operator's terminal through an error message.
const maxBody = 64 << 10

// StatusError is a reply a provider refused with.
//
// It carries the body as well as the status because the body is where the
// provider says *why* — docs/ddns.md §7 asks for `badauth` named as such rather
// than folded into "the update failed". Exported with the body reachable so a
// provider whose refusals are structured JSON can decode it into a sentence,
// which is what Alibaba's does; the default rendering is the raw text, which is
// the honest answer for the ones that are not.
type StatusError struct {
	Host   string
	Status string
	Body   []byte
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("%s said %s: %s", e.Host, e.Status, snippet(e.Body))
}

// do runs one request and decodes the reply.
//
// The status rule is upstream's and is worth keeping: anything at 300 or above
// is a failure. What it is not is the *only* rule — Cloudflare and Tencent
// Cloud both answer 200 with a refusal in the body, so each of them checks its
// own envelope as well.
//
// result may be nil for a provider that reports failure by status alone.
func do(ctx context.Context, client *http.Client, req *http.Request, result any) error {
	resp, err := client.Do(req.WithContext(ctx))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return fmt.Errorf("reading the reply: %w", err)
	}
	if resp.StatusCode >= 300 {
		return &StatusError{Host: req.URL.Host, Status: resp.Status, Body: body}
	}
	if result == nil || len(body) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, result); err != nil {
		return fmt.Errorf("%s answered with something that is not JSON: %s", req.URL.Host, snippet(body))
	}
	return nil
}

// snippet trims a reply down to something an error message can carry.
func snippet(body []byte) string {
	const limit = 400
	s := strings.TrimSpace(string(body))
	if s == "" {
		return "(empty reply)"
	}
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > limit {
		return s[:limit] + "…"
	}
	return s
}
