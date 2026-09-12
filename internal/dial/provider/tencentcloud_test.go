package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func tencentRecordFixture() Record {
	return Record{
		Name:  "home.example.net",
		Zone:  "example.net",
		Type:  "A",
		Value: "203.0.113.9",
		TTL:   600,
		KeyID: "AKIDexample",
		Token: "secret",
	}
}

// tencentServer dispatches on X-TC-Action, which is where this API puts the
// method name.
func tencentServer(t *testing.T, rec *recorder, list any, write http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := rec.record(r)
		w.Header().Set("Content-Type", "application/json")

		if c.Header.Get("X-TC-Action") == "DescribeRecordList" {
			_ = json.NewEncoder(w).Encode(list)
			return
		}
		write(w, r)
	}))
}

func tencentOK(w http.ResponseWriter, _ *http.Request) {
	_, _ = w.Write([]byte(`{"Response":{"RequestId":"x"}}`))
}

func tencentList(records ...tencentCloudRecord) tencentCloudRecordListResp {
	var resp tencentCloudRecordListResp
	resp.Response.RecordList = records
	return resp
}

func TestTencentCloudCreatesAMissingRecord(t *testing.T) {
	rec := &recorder{t: t}
	srv := tencentServer(t, rec, tencentList(), tencentOK)
	defer srv.Close()

	tc := tencentCloud{client: clientFor(t, srv)}
	if err := tc.Update(context.Background(), tencentRecordFixture()); err != nil {
		t.Fatalf("Update: %v", err)
	}

	created := rec.calls[len(rec.calls)-1]
	if got := created.Header.Get("X-TC-Action"); got != "CreateRecord" {
		t.Fatalf("X-TC-Action = %q", got)
	}
	if got := created.Header.Get("X-TC-Version"); got != tencentCloudVersion {
		t.Errorf("X-TC-Version = %q, want %q", got, tencentCloudVersion)
	}

	var body map[string]any
	if err := json.Unmarshal([]byte(created.Body), &body); err != nil {
		t.Fatal(err)
	}
	// CreateRecord takes `SubDomain`; the list request takes `Subdomain`. They
	// are different keys in the same API and sending the wrong one is refused.
	if body["SubDomain"] != "home" {
		t.Errorf("SubDomain = %v, want \"home\"", body["SubDomain"])
	}
	if _, wrong := body["Subdomain"]; wrong {
		t.Error("CreateRecord was sent the list request's spelling of the field")
	}
	if body["RecordLine"] != tencentCloudDefaultLine {
		t.Errorf("RecordLine = %v; a record is addressed by line as well as by name", body["RecordLine"])
	}
	if body["Value"] != "203.0.113.9" {
		t.Errorf("Value = %v", body["Value"])
	}
}

func TestTencentCloudListUsesTheOtherSpelling(t *testing.T) {
	rec := &recorder{t: t}
	srv := tencentServer(t, rec, tencentList(), tencentOK)
	defer srv.Close()

	tc := tencentCloud{client: clientFor(t, srv)}
	if err := tc.Update(context.Background(), tencentRecordFixture()); err != nil {
		t.Fatal(err)
	}

	var body map[string]any
	if err := json.Unmarshal([]byte(rec.calls[0].Body), &body); err != nil {
		t.Fatal(err)
	}
	if body["Subdomain"] != "home" {
		t.Errorf("Subdomain = %v, want \"home\"", body["Subdomain"])
	}
	if _, wrong := body["SubDomain"]; wrong {
		t.Error("DescribeRecordList was sent the write requests' spelling of the field")
	}
}

// The signature covers exactly three headers in a fixed order, and the
// Authorization header has to name them.
func TestTencentCloudSignatureHeaders(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, tencentCloudAPI, strings.NewReader(`{"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	signTencentCloud("AKIDexample", "secret", req, "DescribeRecordList", `{"a":1}`, tencentCloudService)

	auth := req.Header.Get("Authorization")
	switch {
	case !strings.HasPrefix(auth, "TC3-HMAC-SHA256 "):
		t.Errorf("Authorization = %q", auth)
	case !strings.Contains(auth, "Credential=AKIDexample/"):
		t.Errorf("Authorization does not carry the key ID: %q", auth)
	case !strings.Contains(auth, "SignedHeaders=content-type;host;x-tc-action"):
		t.Errorf("Authorization signs the wrong headers: %q", auth)
	case strings.Contains(auth, "secret"):
		t.Error("the secret key reached the Authorization header")
	}
	if req.Header.Get("X-TC-Timestamp") == "" {
		t.Error("no X-TC-Timestamp was set, so the signature cannot be checked")
	}
}

func TestTencentCloudWritesNothingWhenAlreadyCorrect(t *testing.T) {
	rec := &recorder{t: t}
	srv := tencentServer(t, rec,
		tencentList(tencentCloudRecord{RecordId: 1, Value: "203.0.113.9"}),
		func(http.ResponseWriter, *http.Request) { t.Error("wrote to a record that was already correct") })
	defer srv.Close()

	tc := tencentCloud{client: clientFor(t, srv)}
	if err := tc.Update(context.Background(), tencentRecordFixture()); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(rec.calls) != 1 {
		t.Errorf("made %d calls, want only the lookup", len(rec.calls))
	}
}

// "This name has no records" arrives as a refusal in the envelope rather than
// as an empty list, so the first publish of a new name depends on it being let
// through.
func TestTencentCloudTreatsNoRecordsAsEmpty(t *testing.T) {
	rec := &recorder{t: t}
	list := tencentList()
	list.Response.Error = tencentCloudFault{
		Code: tencentCloudNoRecords, Message: "No data of record.",
	}

	srv := tencentServer(t, rec, list, tencentOK)
	defer srv.Close()

	tc := tencentCloud{client: clientFor(t, srv)}
	if err := tc.Update(context.Background(), tencentRecordFixture()); err != nil {
		t.Fatalf("a name with no records yet should be created, got %v", err)
	}
	if rec.count("POST", "/") < 2 {
		t.Error("no create was attempted")
	}
}

// Every other refusal on the list is reported. Tencent Cloud answers 200 with
// the error in the body, so without this a revoked key reads as an empty list
// and olr tries to create a record that already exists.
func TestTencentCloudReportsARefusedLookup(t *testing.T) {
	rec := &recorder{t: t}
	list := tencentList()
	list.Response.Error = tencentCloudFault{
		Code: "AuthFailure.SignatureFailure", Message: "The provided credentials could not be validated.",
	}

	srv := tencentServer(t, rec, list, func(http.ResponseWriter, *http.Request) {
		t.Error("tried to write after the lookup was refused")
	})
	defer srv.Close()

	tc := tencentCloud{client: clientFor(t, srv)}
	err := tc.Update(context.Background(), tencentRecordFixture())
	if err == nil {
		t.Fatal("a 200 carrying an error is a failure")
	}
	if !strings.Contains(err.Error(), "could not be validated") ||
		!strings.Contains(err.Error(), "AuthFailure.SignatureFailure") {
		t.Errorf("err = %v; want the provider's own code and message", err)
	}
}

func TestTencentCloudRefusesARecordSet(t *testing.T) {
	rec := &recorder{t: t}
	srv := tencentServer(t, rec, tencentList(
		tencentCloudRecord{RecordId: 1, Value: "198.51.100.1"},
		tencentCloudRecord{RecordId: 2, Value: "198.51.100.2"},
	), func(http.ResponseWriter, *http.Request) { t.Error("wrote to one of two records") })
	defer srv.Close()

	tc := tencentCloud{client: clientFor(t, srv)}
	if err := tc.Update(context.Background(), tencentRecordFixture()); err == nil {
		t.Fatal("two matching records should be refused")
	}
}
