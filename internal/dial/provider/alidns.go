// Ported from ddns-go, dns/alidns.go with the signer from
// util/aliyun_signer.go and util/aliyun_signer_util.go, at commit
// 7aad574de4ba1e4646f07648a38235d34bab6648.
//
//	Copyright (c) 2020 jeessy
//	SPDX-License-Identifier: MIT
//	Licence text: ./LICENSE.ddns-go
//
// The endpoint, the API version, the three actions, the response structs and
// the signature recipe are upstream's. The driver is ours — see the package
// comment.

package provider

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	// alidnsEndpoint is the Alibaba Cloud DNS API. One host, one path, every
	// action selected by a query parameter — the RPC style the 2015 API uses.
	alidnsEndpoint = "https://alidns.aliyuncs.com/"

	// alidnsVersion is the API version the signature is computed over, so it is
	// not a cosmetic constant: changing it invalidates every signature.
	alidnsVersion = "2015-01-09"

	// alidnsDefaultTTL is upstream's default, in seconds. Alibaba rejects a
	// missing TTL on a free account, so a Record that did not ask for one still
	// has to send this.
	alidnsDefaultTTL = 600
)

type alidns struct{ client *http.Client }

type alidnsRecord struct {
	RecordID string `json:"RecordId"`
	Value    string `json:"Value"`
}

type alidnsSubDomainRecords struct {
	TotalCount    int `json:"TotalCount"`
	DomainRecords struct {
		Record []alidnsRecord `json:"Record"`
	} `json:"DomainRecords"`
}

type alidnsResp struct {
	RecordID string `json:"RecordId"`
}

// alidnsFault is the shape of a refusal. Alibaba answers 4xx with this, so it
// arrives as a StatusError and is decoded back out of it — see explain.
type alidnsFault struct {
	Code    string `json:"Code"`
	Message string `json:"Message"`
}

// Update implements Provider.
func (ali alidns) Update(ctx context.Context, rec Record) error {
	params := url.Values{}
	params.Set("Action", "DescribeSubDomainRecords")
	params.Set("DomainName", rec.Zone)
	params.Set("SubDomain", rec.Name)
	params.Set("Type", rec.Type)

	var records alidnsSubDomainRecords
	if err := ali.request(ctx, rec, params, &records); err != nil {
		return fmt.Errorf("looking up %s at Alibaba Cloud DNS: %w", rec.Name, explainAlidns(err))
	}

	switch n := len(records.DomainRecords.Record); {
	case n == 0:
		return ali.create(ctx, rec)
	case n == 1:
		return ali.modify(ctx, rec, records.DomainRecords.Record[0])
	default:
		return ErrTooManyRecords{Name: rec.Name, Type: rec.Type, Count: n}
	}
}

func (ali alidns) create(ctx context.Context, rec Record) error {
	params := url.Values{}
	params.Set("Action", "AddDomainRecord")
	params.Set("DomainName", rec.Zone)
	params.Set("RR", rec.Subdomain())
	params.Set("Type", rec.Type)
	params.Set("Value", rec.Value)
	params.Set("TTL", strconv.Itoa(ali.ttl(rec)))

	var result alidnsResp
	if err := ali.request(ctx, rec, params, &result); err != nil {
		return fmt.Errorf("creating %s at Alibaba Cloud DNS: %w", rec.Name, explainAlidns(err))
	}
	// Upstream's check, kept: a 200 with no RecordId is Alibaba accepting the
	// request and creating nothing, which would otherwise read as success.
	if result.RecordID == "" {
		return fmt.Errorf("Alibaba Cloud DNS accepted the request to create %s but returned no record ID", rec.Name)
	}
	return nil
}

func (ali alidns) modify(ctx context.Context, rec Record, have alidnsRecord) error {
	if have.Value == rec.Value {
		return nil
	}

	params := url.Values{}
	params.Set("Action", "UpdateDomainRecord")
	params.Set("RecordId", have.RecordID)
	params.Set("RR", rec.Subdomain())
	params.Set("Type", rec.Type)
	params.Set("Value", rec.Value)
	params.Set("TTL", strconv.Itoa(ali.ttl(rec)))

	var result alidnsResp
	if err := ali.request(ctx, rec, params, &result); err != nil {
		return fmt.Errorf("updating %s at Alibaba Cloud DNS: %w", rec.Name, explainAlidns(err))
	}
	if result.RecordID == "" {
		return fmt.Errorf("Alibaba Cloud DNS accepted the request to update %s but returned no record ID", rec.Name)
	}
	return nil
}

func (ali alidns) ttl(rec Record) int {
	if rec.TTL <= 0 {
		return alidnsDefaultTTL
	}
	return rec.TTL
}

// explainAlidns turns a refusal into the provider's own sentence.
//
// Without it the operator gets `alidns.aliyuncs.com said 404 Not Found:
// {"RequestId":…,"HostId":…,"Code":"InvalidAccessKeyId.NotFound",…}`, which
// contains the answer and buries it. With it they get the code and the message,
// which is what docs/ddns.md §7's "credential wrong or revoked" row asks for.
func explainAlidns(err error) error {
	var status *StatusError
	if !errors.As(err, &status) {
		return err
	}
	var fault alidnsFault
	if json.Unmarshal(status.Body, &fault) != nil || fault.Code == "" {
		return err
	}
	if fault.Message == "" {
		return errors.New(fault.Code)
	}
	return fmt.Errorf("%s (%s)", fault.Message, fault.Code)
}

// request signs and sends one call.
func (ali alidns) request(ctx context.Context, rec Record, params url.Values, result any) error {
	signAliyun(rec.KeyID, rec.Token, params, http.MethodGet, alidnsVersion)

	req, err := http.NewRequest(http.MethodGet, alidnsEndpoint, nil)
	if err != nil {
		return err
	}
	req.URL.RawQuery = params.Encode()

	return do(ctx, ali.client, req, result)
}

// --- signature --------------------------------------------------------------

// signAliyun adds the common parameters and the signature, in place.
//
// The algorithm is Alibaba's RPC signature v1.0 and every byte of it is
// load-bearing: HMAC-SHA1 over `METHOD&%2F&<doubly-encoded sorted query>`, with
// the key being the access secret followed by a literal `&`.
//
// Upstream builds the string to sign by percent-encoding the output of
// url.Values.Encode a second time, character by character, through a channel
// and a goroutine. This does the same thing by encoding each key and value and
// joining them, which is how Alibaba's own documented example writes it. The
// two agree on every input this module can produce, and this one also agrees on
// the one they differ on — a value containing a space, which Encode renders as
// `+` and the signature requires as `%20`.
func signAliyun(keyID, secret string, params url.Values, method, version string) {
	now := time.Now()

	params.Set("Format", "JSON")
	params.Set("Version", version)
	params.Set("AccessKeyId", keyID)
	params.Set("SignatureMethod", "HMAC-SHA1")
	params.Set("SignatureVersion", "1.0")
	params.Set("SignatureNonce", strconv.FormatInt(now.UnixNano(), 10))
	params.Set("Timestamp", now.UTC().Format("2006-01-02T15:04:05Z"))

	stringToSign := method + "&" + aliyunEncode("/") + "&" + aliyunEncode(canonicalQuery(params))

	mac := hmac.New(sha1.New, []byte(secret+"&"))
	mac.Write([]byte(stringToSign))
	params.Set("Signature", base64.StdEncoding.EncodeToString(mac.Sum(nil)))
}

// canonicalQuery renders the parameters sorted by key, each half encoded.
func canonicalQuery(params url.Values) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, aliyunEncode(k)+"="+aliyunEncode(params.Get(k)))
	}
	return strings.Join(parts, "&")
}

// aliyunEncode is RFC 3986 percent-encoding, which differs from Go's query
// encoding in exactly three places — and all three of them change the
// signature, so none of them is cosmetic.
func aliyunEncode(s string) string {
	e := url.QueryEscape(s)
	e = strings.ReplaceAll(e, "+", "%20")
	e = strings.ReplaceAll(e, "*", "%2A")
	e = strings.ReplaceAll(e, "%7E", "~")
	return e
}
