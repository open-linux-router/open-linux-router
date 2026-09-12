// Ported from ddns-go, dns/callback.go, at commit
// 7aad574de4ba1e4646f07648a38235d34bab6648.
//
//	Copyright (c) 2020 jeessy
//	SPDX-License-Identifier: MIT
//	Licence text: ./LICENSE.ddns-go
//
// The `#{…}` template syntax, the placeholder names and the GET-unless-there-is
// -a-body rule are upstream's, so a URL an operator already has working in
// ddns-go can be pasted here unchanged. The driver is ours — see the package
// comment.

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// callback is the escape hatch, and it is why four providers cover more than
// four services.
//
// The DynDNS v2 protocol is one GET — `/nic/update?hostname=…&myip=…` with
// basic auth in the URL — and No-IP, DuckDNS, Dynu, afraid.org and deSEC all
// speak some form of it. A URL template covers every one of them without a file
// per vendor, which is what docs/ddns.md §10 means by the long tail being
// reachable with one implementation.
//
// What it cannot do is read anything back. There is no record to look up, so
// there is no "already correct, nothing to do" and no way to tell a name that
// does not exist from one that does. Success here is the endpoint answering
// below 300, which is as much as the protocol says.
type callback struct{ client *http.Client }

// Update implements Provider.
func (cb callback) Update(ctx context.Context, rec Record) error {
	target := cb.expand(rec.URL, rec)
	if _, err := url.Parse(target); err != nil {
		// Almost always a template that expanded into something unparseable
		// rather than one that was wrong when it was typed, so the error names
		// the expansion rather than the field.
		return fmt.Errorf("the callback URL for %s did not expand into a URL: %w", rec.Name, err)
	}

	method, body := http.MethodGet, ""
	contentType := "application/x-www-form-urlencoded"
	if rec.Token != "" {
		method = http.MethodPost
		body = cb.expand(rec.Token, rec)
		// Upstream's sniff, kept: the same field carries a form body for one
		// endpoint and a JSON document for the next, and asking the operator to
		// declare which would be a field that can only ever disagree with the
		// value beside it.
		if json.Valid([]byte(body)) {
			contentType = "application/json"
		}
	}

	req, err := http.NewRequest(method, target, strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)

	// No result to decode: what comes back is `good 203.0.113.9`, or `nochg`, or
	// an HTML page, depending on whose endpoint it is. do reports anything at
	// 300 or above as a failure and carries the body into the error, which is
	// the whole of what this protocol offers.
	if err := do(ctx, cb.client, req, nil); err != nil {
		return fmt.Errorf("calling back for %s: %w", rec.Name, err)
	}
	return nil
}

// expand substitutes the `#{…}` placeholders.
//
// The names are upstream's so that an existing ddns-go URL works unchanged.
// `#{ipv6Addr}` is not among them: v1 publishes an A record and nothing else
// (docs/ddns.md §9 #2), so a template mentioning it would silently expand to an
// empty string, and leaving it unsubstituted is the version of that failure an
// operator can see.
func (cb callback) expand(template string, rec Record) string {
	ttl := rec.TTL
	if ttl <= 0 {
		ttl = callbackDefaultTTL
	}

	return strings.NewReplacer(
		"#{ip}", rec.Value,
		"#{ipv4Addr}", rec.Value,
		"#{domain}", rec.Name,
		"#{recordType}", rec.Type,
		"#{ttl}", strconv.Itoa(ttl),
		"#{timestamp}", strconv.FormatInt(time.Now().UTC().Unix(), 10),
	).Replace(template)
}

// callbackDefaultTTL is upstream's, and is only ever seen by an endpoint whose
// template asks for `#{ttl}`. Most do not.
const callbackDefaultTTL = 600
