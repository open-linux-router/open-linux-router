// Ported from ddns-go, dns/tencent_cloud.go with the signer from
// util/tencent_cloud_signer.go, at commit
// 7aad574de4ba1e4646f07648a38235d34bab6648.
//
//	Copyright (c) 2020 jeessy
//	SPDX-License-Identifier: MIT
//	Licence text: ./LICENSE.ddns-go
//
// The endpoint, the API version, the three actions, the request and response
// structs and the TC3-HMAC-SHA256 recipe are upstream's. The driver is ours —
// see the package comment.

package provider

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	// tencentCloudService is both the subdomain of the endpoint and a field in
	// the signature's credential scope, which is why it is one constant rather
	// than two strings that could disagree.
	tencentCloudService = "dnspod"
	tencentCloudHost    = tencentCloudService + ".tencentcloudapi.com"
	tencentCloudAPI     = "https://" + tencentCloudHost

	// tencentCloudVersion selects the DNSPod API generation. 2021-03-23 is the
	// one whose actions are named below.
	tencentCloudVersion = "2021-03-23"

	// tencentCloudDefaultTTL is upstream's default, in seconds.
	tencentCloudDefaultTTL = 600

	// tencentCloudDefaultLine is DNSPod's default resolution line, and it is
	// literally this Chinese word in the API. A record is addressed by line as
	// well as by name and type, so sending the wrong one creates a second
	// record beside the operator's rather than updating it.
	tencentCloudDefaultLine = "默认"

	// tencentCloudNoRecords is what DescribeRecordList answers with when the
	// name has no records yet. It is a refusal in the envelope rather than an
	// empty list, so without this constant the first publish of a new name
	// would report an error instead of creating it.
	tencentCloudNoRecords = "ResourceNotFound.NoDataOfRecord"
)

type tencentCloud struct{ client *http.Client }

// tencentCloudRecord is one record, and the field set is upstream's including
// its two spellings of "subdomain" — DescribeRecordList takes `Subdomain`,
// CreateRecord and ModifyRecord take `SubDomain`, and the API rejects the other
// one. That is not a typo to tidy up.
type tencentCloudRecord struct {
	Domain     string `json:"Domain"`
	SubDomain  string `json:"SubDomain,omitempty"`
	Subdomain  string `json:"Subdomain,omitempty"`
	RecordType string `json:"RecordType"`
	RecordLine string `json:"RecordLine"`
	Value      string `json:"Value,omitempty"`
	RecordId   int64  `json:"RecordId,omitempty"`
	TTL        int    `json:"TTL,omitempty"`
}

// tencentCloudFault is a refusal. Tencent Cloud answers 200 with one of these
// in the envelope, so the HTTP status alone would report every refusal as a
// success.
type tencentCloudFault struct {
	Code    string `json:"Code"`
	Message string `json:"Message"`
}

func (f tencentCloudFault) err() error {
	switch {
	case f.Code == "":
		return nil
	case f.Message == "":
		return fmt.Errorf("%s", f.Code)
	default:
		return fmt.Errorf("%s (%s)", f.Message, f.Code)
	}
}

// tencentCloudStatus is the envelope for the two actions that return nothing
// but success or failure.
type tencentCloudStatus struct {
	Response struct {
		Error tencentCloudFault `json:"Error"`
	} `json:"Response"`
}

func (s tencentCloudStatus) fault() error { return s.Response.Error.err() }

// tencentCloudRecordListResp spells Error out again rather than embedding
// tencentCloudStatus, which is where this file deliberately diverges from
// upstream.
//
// Embedding would put two fields named Response in one struct, and
// encoding/json resolves that conflict in favour of the shallower one — so the
// embedded Error would never be populated and every refusal of a list request
// would decode as an empty list. Upstream has the embedding and does not notice
// because it never reads the error off a list; we read it, so we cannot have
// it.
// The reply also carries RecordCountInfo.TotalCount, which upstream branches
// on. It is left out because it counts what matched across every page, and the
// list below is one page — so on the one input where the two differ, the count
// says "there is a record" and the list we would then index into is empty.
type tencentCloudRecordListResp struct {
	Response struct {
		Error      tencentCloudFault    `json:"Error"`
		RecordList []tencentCloudRecord `json:"RecordList"`
	} `json:"Response"`
}

func (r tencentCloudRecordListResp) fault() error { return r.Response.Error.err() }

// Update implements Provider.
func (tc tencentCloud) Update(ctx context.Context, rec Record) error {
	list := tencentCloudRecordListResp{}
	err := tc.request(ctx, rec, "DescribeRecordList", tencentCloudRecord{
		Domain:     rec.Zone,
		Subdomain:  rec.Subdomain(),
		RecordType: rec.Type,
		RecordLine: tencentCloudDefaultLine,
	}, &list)
	if err != nil {
		return fmt.Errorf("looking up %s at Tencent Cloud DNSPod: %w", rec.Name, err)
	}
	if fault := list.fault(); fault != nil && list.Response.Error.Code != tencentCloudNoRecords {
		// Every refusal except "this name has no records yet", which is how the
		// API reports an empty list and is the normal first publish of a name.
		return fmt.Errorf("looking up %s at Tencent Cloud DNSPod: %w", rec.Name, fault)
	}

	switch n := len(list.Response.RecordList); {
	case n == 0:
		return tc.create(ctx, rec)
	case n == 1:
		return tc.modify(ctx, rec, list.Response.RecordList[0])
	default:
		return ErrTooManyRecords{Name: rec.Name, Type: rec.Type, Count: n}
	}
}

func (tc tencentCloud) create(ctx context.Context, rec Record) error {
	var status tencentCloudStatus
	err := tc.request(ctx, rec, "CreateRecord", tencentCloudRecord{
		Domain:     rec.Zone,
		SubDomain:  rec.Subdomain(),
		RecordType: rec.Type,
		RecordLine: tencentCloudDefaultLine,
		Value:      rec.Value,
		TTL:        tc.ttl(rec),
	}, &status)
	if err != nil {
		return fmt.Errorf("creating %s at Tencent Cloud DNSPod: %w", rec.Name, err)
	}
	if fault := status.fault(); fault != nil {
		return fmt.Errorf("Tencent Cloud DNSPod refused to create %s: %w", rec.Name, fault)
	}
	return nil
}

func (tc tencentCloud) modify(ctx context.Context, rec Record, have tencentCloudRecord) error {
	if have.Value == rec.Value {
		return nil
	}

	var status tencentCloudStatus
	err := tc.request(ctx, rec, "ModifyRecord", tencentCloudRecord{
		Domain:     rec.Zone,
		SubDomain:  rec.Subdomain(),
		RecordType: rec.Type,
		RecordLine: tencentCloudDefaultLine,
		Value:      rec.Value,
		RecordId:   have.RecordId,
		TTL:        tc.ttl(rec),
	}, &status)
	if err != nil {
		return fmt.Errorf("updating %s at Tencent Cloud DNSPod: %w", rec.Name, err)
	}
	if fault := status.fault(); fault != nil {
		return fmt.Errorf("Tencent Cloud DNSPod refused to update %s: %w", rec.Name, fault)
	}
	return nil
}

func (tc tencentCloud) ttl(rec Record) int {
	if rec.TTL <= 0 {
		return tencentCloudDefaultTTL
	}
	return rec.TTL
}

// request signs and sends one action.
func (tc tencentCloud) request(ctx context.Context, rec Record, action string, body, result any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, tencentCloudAPI, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-TC-Version", tencentCloudVersion)
	signTencentCloud(rec.KeyID, rec.Token, req, action, string(payload), tencentCloudService)

	return do(ctx, tc.client, req, result)
}

// --- signature --------------------------------------------------------------

// signTencentCloud adds the TC3-HMAC-SHA256 headers, in place.
//
// Upstream's, transcribed. The one thing worth noticing when reading it is that
// the canonical request hard-codes the three signed headers and their order —
// content-type, host, x-tc-action — so adding a header to the request above
// without adding it here silently invalidates every signature.
func signTencentCloud(secretID, secretKey string, r *http.Request, action, payload, service string) {
	const algorithm = "TC3-HMAC-SHA256"

	host := service + ".tencentcloudapi.com"
	now := time.Now()
	timestamp := strconv.FormatInt(now.Unix(), 10)

	// Step 1: the canonical request.
	canonicalHeaders := "content-type:application/json\nhost:" + host +
		"\nx-tc-action:" + strings.ToLower(action) + "\n"
	signedHeaders := "content-type;host;x-tc-action"
	canonicalRequest := "POST\n/\n\n" + canonicalHeaders + "\n" + signedHeaders + "\n" + sha256hex(payload)

	// Step 2: the string to sign.
	date := now.UTC().Format("2006-01-02")
	credentialScope := date + "/" + service + "/tc3_request"
	stringToSign := algorithm + "\n" + timestamp + "\n" + credentialScope + "\n" + sha256hex(canonicalRequest)

	// Step 3: the derived key, one HMAC per scope component.
	secretDate := hmacSHA256(date, "TC3"+secretKey)
	secretService := hmacSHA256(service, secretDate)
	secretSigning := hmacSHA256("tc3_request", secretService)
	signature := hex.EncodeToString([]byte(hmacSHA256(stringToSign, secretSigning)))

	r.Header.Set("Authorization", algorithm+
		" Credential="+secretID+"/"+credentialScope+
		", SignedHeaders="+signedHeaders+
		", Signature="+signature)
	r.Header.Set("Host", host)
	r.Header.Set("X-TC-Action", action)
	r.Header.Set("X-TC-Timestamp", timestamp)
}

func sha256hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// hmacSHA256 returns the raw digest as a string, because the next round uses it
// as a key rather than printing it. Only the last one is hex-encoded.
func hmacSHA256(s, key string) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(s))
	return string(mac.Sum(nil))
}
