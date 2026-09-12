package provider

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func alidnsRecordFixture() Record {
	return Record{
		Name:  "home.example.net",
		Zone:  "example.net",
		Type:  "A",
		Value: "203.0.113.9",
		TTL:   600,
		KeyID: "LTAIexample",
		Token: "secret",
	}
}

// alidnsServer dispatches on the Action parameter, which is how this API works:
// one host, one path, everything selected by a query parameter.
func alidnsServer(t *testing.T, rec *recorder, records []alidnsRecord, write http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := rec.record(r)
		w.Header().Set("Content-Type", "application/json")

		switch c.Query.Get("Action") {
		case "DescribeSubDomainRecords":
			resp := alidnsSubDomainRecords{TotalCount: len(records)}
			resp.DomainRecords.Record = records
			_ = json.NewEncoder(w).Encode(resp)
		default:
			write(w, r)
		}
	}))
}

func alidnsCreated(w http.ResponseWriter, _ *http.Request) {
	_ = json.NewEncoder(w).Encode(alidnsResp{RecordID: "12345"})
}

func TestAlidnsCreatesAMissingRecord(t *testing.T) {
	rec := &recorder{t: t}
	srv := alidnsServer(t, rec, nil, alidnsCreated)
	defer srv.Close()

	ali := alidns{client: clientFor(t, srv)}
	if err := ali.Update(context.Background(), alidnsRecordFixture()); err != nil {
		t.Fatalf("Update: %v", err)
	}

	created := rec.calls[len(rec.calls)-1]
	if got := created.Query.Get("Action"); got != "AddDomainRecord" {
		t.Fatalf("Action = %q", got)
	}
	// RR is the label below the zone, not the whole name: sending
	// "home.example.net" here creates "home.example.net.example.net".
	for field, want := range map[string]string{
		"DomainName": "example.net",
		"RR":         "home",
		"Type":       "A",
		"Value":      "203.0.113.9",
		"TTL":        "600",
	} {
		if got := created.Query.Get(field); got != want {
			t.Errorf("%s = %q, want %q", field, got, want)
		}
	}
	// The secret is never a parameter; only the signature derived from it is.
	if strings.Contains(created.Query.Encode(), "secret") {
		t.Error("the access secret reached the query string")
	}
}

// The signature is the one thing here that is wrong in a way nothing else
// catches: a string-to-sign that is one character out authenticates against
// nothing, and the vendor's answer says only "SignatureDoesNotMatch".
func TestAlidnsSignatureMatchesTheDocumentedRecipe(t *testing.T) {
	params := url.Values{}
	params.Set("Action", "DescribeSubDomainRecords")
	params.Set("DomainName", "example.net")
	params.Set("SubDomain", "home.example.net")

	signAliyun("LTAIexample", "secret", params, http.MethodGet, alidnsVersion)

	signature := params.Get("Signature")
	if signature == "" {
		t.Fatal("no signature was set")
	}

	// Recompute independently, the way Alibaba's own documentation states it.
	unsigned := url.Values{}
	for k, v := range params {
		if k != "Signature" {
			unsigned[k] = v
		}
	}
	want := hmacSHA1("secret&", http.MethodGet+"&"+aliyunEncode("/")+"&"+aliyunEncode(canonicalQuery(unsigned)))
	if signature != want {
		t.Errorf("Signature = %q, want %q", signature, want)
	}

	for _, common := range []string{"AccessKeyId", "SignatureMethod", "SignatureNonce",
		"SignatureVersion", "Timestamp", "Format", "Version"} {
		if params.Get(common) == "" {
			t.Errorf("the signed parameters are missing %s", common)
		}
	}
	if got := params.Get("Version"); got != alidnsVersion {
		t.Errorf("Version = %q, want %q", got, alidnsVersion)
	}
}

func hmacSHA1(key, message string) string {
	mac := hmac.New(sha1.New, []byte(key))
	mac.Write([]byte(message))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// The three places Go's query encoding and RFC 3986 differ, each of which
// changes the signature.
func TestAliyunEncodeIsRFC3986(t *testing.T) {
	for in, want := range map[string]string{
		"a b":   "a%20b",
		"a*b":   "a%2Ab",
		"a~b":   "a~b",
		"a/b":   "a%2Fb",
		"a=b&c": "a%3Db%26c",
		"a-_.b": "a-_.b",
	} {
		if got := aliyunEncode(in); got != want {
			t.Errorf("aliyunEncode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAlidnsWritesNothingWhenAlreadyCorrect(t *testing.T) {
	rec := &recorder{t: t}
	srv := alidnsServer(t, rec,
		[]alidnsRecord{{RecordID: "r1", Value: "203.0.113.9"}},
		func(http.ResponseWriter, *http.Request) { t.Error("wrote to a record that was already correct") })
	defer srv.Close()

	ali := alidns{client: clientFor(t, srv)}
	if err := ali.Update(context.Background(), alidnsRecordFixture()); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(rec.calls) != 1 {
		t.Errorf("made %d calls, want only the lookup", len(rec.calls))
	}
}

func TestAlidnsUpdatesByRecordID(t *testing.T) {
	rec := &recorder{t: t}
	srv := alidnsServer(t, rec,
		[]alidnsRecord{{RecordID: "r1", Value: "198.51.100.1"}},
		func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(alidnsResp{RecordID: "r1"})
		})
	defer srv.Close()

	ali := alidns{client: clientFor(t, srv)}
	if err := ali.Update(context.Background(), alidnsRecordFixture()); err != nil {
		t.Fatalf("Update: %v", err)
	}

	updated := rec.calls[len(rec.calls)-1]
	if got := updated.Query.Get("Action"); got != "UpdateDomainRecord" {
		t.Fatalf("Action = %q", got)
	}
	if got := updated.Query.Get("RecordId"); got != "r1" {
		t.Errorf("RecordId = %q", got)
	}
}

func TestAlidnsRefusesARecordSet(t *testing.T) {
	rec := &recorder{t: t}
	srv := alidnsServer(t, rec, []alidnsRecord{
		{RecordID: "r1", Value: "198.51.100.1"},
		{RecordID: "r2", Value: "198.51.100.2"},
	}, func(http.ResponseWriter, *http.Request) { t.Error("wrote to one of two records") })
	defer srv.Close()

	ali := alidns{client: clientFor(t, srv)}
	if err := ali.Update(context.Background(), alidnsRecordFixture()); err == nil {
		t.Fatal("two matching records should be refused")
	}
}

// Alibaba answers a bad credential with 404 and a JSON fault. Without the
// decoding the operator gets the whole envelope; with it, the sentence.
func TestAlidnsReportsItsOwnRefusal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"RequestId": "abc",
			"HostId":    "alidns.aliyuncs.com",
			"Code":      "InvalidAccessKeyId.NotFound",
			"Message":   "Specified access key is not found.",
		})
	}))
	defer srv.Close()

	ali := alidns{client: clientFor(t, srv)}
	err := ali.Update(context.Background(), alidnsRecordFixture())
	if err == nil {
		t.Fatal("a 404 is a failure")
	}
	if !strings.Contains(err.Error(), "Specified access key is not found") ||
		!strings.Contains(err.Error(), "InvalidAccessKeyId.NotFound") {
		t.Errorf("err = %v; want the provider's own code and message", err)
	}
}

// A 200 with no record ID is Alibaba accepting the request and creating
// nothing, which would otherwise read as success.
func TestAlidnsRejectsAnEmptyRecordID(t *testing.T) {
	rec := &recorder{t: t}
	srv := alidnsServer(t, rec, nil, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(alidnsResp{})
	})
	defer srv.Close()

	ali := alidns{client: clientFor(t, srv)}
	if err := ali.Update(context.Background(), alidnsRecordFixture()); err == nil {
		t.Fatal("a create that returned no record ID should not report success")
	}
}
