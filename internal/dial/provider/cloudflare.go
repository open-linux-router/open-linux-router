// Ported from ddns-go, dns/cloudflare.go, at commit
// 7aad574de4ba1e4646f07648a38235d34bab6648.
//
//	Copyright (c) 2020 jeessy
//	SPDX-License-Identifier: MIT
//	Licence text: ./LICENSE.ddns-go
//
// The endpoint, the zone lookup, the request and response structs and the
// bearer header are upstream's. The driver is ours — see the package comment.

package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// zonesAPI is Cloudflare's v4 zones endpoint, which every request here hangs
// off: a record is addressed by zone ID, and the zone ID is only reachable by
// looking the zone name up first.
const zonesAPI = "https://api.cloudflare.com/client/v4/zones"

// cloudflareAutoTTL is Cloudflare's "let us decide" TTL. It is 1 rather than 0,
// and 0 is rejected — so a Record that did not ask for a TTL has to be given
// this instead of being left empty.
const cloudflareAutoTTL = 1

type cloudflare struct{ client *http.Client }

// cloudflareStatus is the envelope every v4 reply carries.
//
// Success is the field that matters and the reason this type is separate:
// Cloudflare answers 200 with `"success": false` for a rejected change, so the
// HTTP status alone would report an update that did not happen.
type cloudflareStatus struct {
	Success  bool              `json:"success"`
	Errors   []cloudflareError `json:"errors"`
	Messages []any             `json:"messages"`
}

type cloudflareError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// reason renders the provider's own words, which docs/ddns.md §7 asks for by
// name: "the credential is wrong" is our guess, "Invalid API Token" is theirs.
func (s cloudflareStatus) reason() string {
	if len(s.Errors) == 0 {
		return "no reason given"
	}
	out := make([]string, 0, len(s.Errors))
	for _, e := range s.Errors {
		out = append(out, fmt.Sprintf("%s (code %d)", e.Message, e.Code))
	}
	return strings.Join(out, "; ")
}

type cloudflareZonesResp struct {
	cloudflareStatus
	Result []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"result"`
}

type cloudflareRecordsResp struct {
	cloudflareStatus
	Result []cloudflareRecord `json:"result"`
}

type cloudflareRecord struct {
	ID      string `json:"id,omitempty"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Content string `json:"content"`
	Proxied bool   `json:"proxied"`
	TTL     int    `json:"ttl"`
}

// Update implements Provider.
//
// Three requests at most: find the zone, find the record, write it. Upstream
// makes the same three and then updates *every* matching record; this refuses
// past the first, for the reason the package comment gives.
//
// A name with non-ASCII labels has to be given in its punycode form. Upstream
// converts, because its config accepts either; ours does not convert, because
// the conversion needs golang.org/x/text and the whole module would be carried
// for the handful of operators who have one. The validator says so rather than
// letting Cloudflare answer "no such record" about a name that looks right.
func (cf cloudflare) Update(ctx context.Context, rec Record) error {
	zoneID, err := cf.zoneID(ctx, rec)
	if err != nil {
		return err
	}

	params := url.Values{}
	params.Set("type", rec.Type)
	params.Set("name", rec.Name)
	params.Set("per_page", "50")

	var records cloudflareRecordsResp
	if err := cf.request(ctx, rec, "GET",
		fmt.Sprintf("%s/%s/dns_records?%s", zonesAPI, zoneID, params.Encode()),
		nil, &records); err != nil {
		return fmt.Errorf("looking up %s at Cloudflare: %w", rec.Name, err)
	}
	if !records.Success {
		return fmt.Errorf("Cloudflare refused to list %s: %s", rec.Name, records.reason())
	}

	switch len(records.Result) {
	case 0:
		return cf.create(ctx, rec, zoneID)
	case 1:
		return cf.modify(ctx, rec, zoneID, records.Result[0])
	default:
		return ErrTooManyRecords{Name: rec.Name, Type: rec.Type, Count: len(records.Result)}
	}
}

// zoneID turns the zone name into the ID every other call needs.
func (cf cloudflare) zoneID(ctx context.Context, rec Record) (string, error) {
	params := url.Values{}
	params.Set("name", rec.Zone)
	params.Set("status", "active")
	params.Set("per_page", "50")

	var zones cloudflareZonesResp
	if err := cf.request(ctx, rec, "GET",
		fmt.Sprintf("%s?%s", zonesAPI, params.Encode()), nil, &zones); err != nil {
		return "", fmt.Errorf("looking up the zone %s at Cloudflare: %w", rec.Zone, err)
	}
	if !zones.Success {
		return "", fmt.Errorf("Cloudflare refused to look up the zone %s: %s", rec.Zone, zones.reason())
	}
	if len(zones.Result) == 0 {
		// The single most common setup mistake, and worth naming precisely: an
		// API token scoped to one zone answers this with an empty list rather
		// than with an authentication failure, so "there is no such zone" and
		// "your token cannot see that zone" arrive identically.
		return "", fmt.Errorf("Cloudflare has no active zone named %s, "+
			"or this token cannot see it", rec.Zone)
	}
	return zones.Result[0].ID, nil
}

func (cf cloudflare) create(ctx context.Context, rec Record, zoneID string) error {
	body := cloudflareRecord{
		Type:    rec.Type,
		Name:    rec.Name,
		Content: rec.Value,
		Proxied: false,
		TTL:     cf.ttl(rec),
	}

	var status cloudflareStatus
	if err := cf.request(ctx, rec, "POST",
		fmt.Sprintf("%s/%s/dns_records", zonesAPI, zoneID), body, &status); err != nil {
		return fmt.Errorf("creating %s at Cloudflare: %w", rec.Name, err)
	}
	if !status.Success {
		return fmt.Errorf("Cloudflare refused to create %s: %s", rec.Name, status.reason())
	}
	return nil
}

func (cf cloudflare) modify(ctx context.Context, rec Record, zoneID string, have cloudflareRecord) error {
	// Already right. This is not the publisher's "the address did not change"
	// check — that one stops us getting here at all — but the answer to the
	// first check after a restart, when our cache is empty and the provider's
	// record is already current (docs/ddns.md §6).
	if have.Content == rec.Value {
		return nil
	}

	// Proxied is carried over rather than set. Whether a name goes through
	// Cloudflare's proxy is a decision the operator made in their dashboard,
	// and a DDNS update that quietly turned it off would take their site's
	// certificate and DDoS protection with it.
	body := cloudflareRecord{
		Type:    rec.Type,
		Name:    rec.Name,
		Content: rec.Value,
		Proxied: have.Proxied,
		TTL:     cf.ttl(rec),
	}

	var status cloudflareStatus
	if err := cf.request(ctx, rec, "PUT",
		fmt.Sprintf("%s/%s/dns_records/%s", zonesAPI, zoneID, have.ID), body, &status); err != nil {
		return fmt.Errorf("updating %s at Cloudflare: %w", rec.Name, err)
	}
	if !status.Success {
		return fmt.Errorf("Cloudflare refused to update %s: %s", rec.Name, status.reason())
	}
	return nil
}

func (cf cloudflare) ttl(rec Record) int {
	if rec.TTL <= 0 {
		return cloudflareAutoTTL
	}
	return rec.TTL
}

// request is the one place a Cloudflare call is built, so the bearer token
// reaches exactly one header and nothing else.
func (cf cloudflare) request(ctx context.Context, rec Record, method, endpoint string, body, result any) error {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return err
		}
	}

	req, err := http.NewRequest(method, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+rec.Token)
	req.Header.Set("Content-Type", "application/json")

	return do(ctx, cf.client, req, result)
}
