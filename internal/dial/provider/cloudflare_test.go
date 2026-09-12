package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func cloudflareRecordFixture() Record {
	return Record{
		Name:  "home.example.net",
		Zone:  "example.net",
		Type:  "A",
		Value: "203.0.113.9",
		TTL:   120,
		Token: "cf-token",
	}
}

// cloudflareServer answers the zone lookup and hands the record list to the
// test, which is the only part that differs between these cases.
func cloudflareServer(t *testing.T, rec *recorder, records []cloudflareRecord, write http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := rec.record(r)
		w.Header().Set("Content-Type", "application/json")

		switch {
		case c.Method == "GET" && c.Path == "/client/v4/zones":
			_ = json.NewEncoder(w).Encode(cloudflareZonesResp{
				cloudflareStatus: cloudflareStatus{Success: true},
				Result: []struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				}{{ID: "zone1", Name: "example.net"}},
			})
		case c.Method == "GET" && c.Path == "/client/v4/zones/zone1/dns_records":
			_ = json.NewEncoder(w).Encode(cloudflareRecordsResp{
				cloudflareStatus: cloudflareStatus{Success: true},
				Result:           records,
			})
		default:
			write(w, r)
		}
	}))
}

func okStatus(w http.ResponseWriter, _ *http.Request) {
	_ = json.NewEncoder(w).Encode(cloudflareStatus{Success: true})
}

// The happy path, asserted on the wire: the token reaches the Authorization
// header and nowhere else, and the record is addressed by zone ID.
func TestCloudflareCreatesAMissingRecord(t *testing.T) {
	rec := &recorder{t: t}
	srv := cloudflareServer(t, rec, nil, okStatus)
	defer srv.Close()

	cf := cloudflare{client: clientFor(t, srv)}
	if err := cf.Update(context.Background(), cloudflareRecordFixture()); err != nil {
		t.Fatalf("Update: %v", err)
	}

	created := rec.calls[len(rec.calls)-1]
	if created.Method != "POST" || created.Path != "/client/v4/zones/zone1/dns_records" {
		t.Fatalf("created via %s %s", created.Method, created.Path)
	}
	if got := created.Header.Get("Authorization"); got != "Bearer cf-token" {
		t.Errorf("Authorization = %q", got)
	}
	var body cloudflareRecord
	if err := json.Unmarshal([]byte(created.Body), &body); err != nil {
		t.Fatal(err)
	}
	if body.Content != "203.0.113.9" || body.Name != "home.example.net" || body.Type != "A" {
		t.Errorf("created %+v", body)
	}
	if body.TTL != 120 {
		t.Errorf("TTL = %d, want the record's 120", body.TTL)
	}
}

// Proxied is carried over rather than reset. A DDNS update that quietly turned
// off Cloudflare's proxy would take the site's certificate with it.
func TestCloudflareUpdateKeepsTheProxiedFlag(t *testing.T) {
	rec := &recorder{t: t}
	srv := cloudflareServer(t, rec,
		[]cloudflareRecord{{ID: "r1", Name: "home.example.net", Type: "A", Content: "198.51.100.1", Proxied: true}},
		okStatus)
	defer srv.Close()

	cf := cloudflare{client: clientFor(t, srv)}
	if err := cf.Update(context.Background(), cloudflareRecordFixture()); err != nil {
		t.Fatalf("Update: %v", err)
	}

	updated := rec.calls[len(rec.calls)-1]
	if updated.Method != "PUT" || !strings.HasSuffix(updated.Path, "/dns_records/r1") {
		t.Fatalf("updated via %s %s", updated.Method, updated.Path)
	}
	var body cloudflareRecord
	if err := json.Unmarshal([]byte(updated.Body), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Proxied {
		t.Error("proxied was reset by an address update")
	}
	if body.Content != "203.0.113.9" {
		t.Errorf("content = %q", body.Content)
	}
}

// The publisher's cache is empty after a restart, so the first check always
// reaches here. Finding the record already correct must be a success with no
// write — otherwise every restart writes to the provider.
func TestCloudflareWritesNothingWhenAlreadyCorrect(t *testing.T) {
	rec := &recorder{t: t}
	srv := cloudflareServer(t, rec,
		[]cloudflareRecord{{ID: "r1", Name: "home.example.net", Type: "A", Content: "203.0.113.9"}},
		func(http.ResponseWriter, *http.Request) { t.Error("wrote to a record that was already correct") })
	defer srv.Close()

	cf := cloudflare{client: clientFor(t, srv)}
	if err := cf.Update(context.Background(), cloudflareRecordFixture()); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if n := rec.count("PUT", "") + rec.count("POST", ""); n != 0 {
		t.Errorf("made %d writes", n)
	}
}

// A round-robin is somebody else's decision and not ours to collapse.
func TestCloudflareRefusesARecordSet(t *testing.T) {
	rec := &recorder{t: t}
	srv := cloudflareServer(t, rec, []cloudflareRecord{
		{ID: "r1", Content: "198.51.100.1"},
		{ID: "r2", Content: "198.51.100.2"},
	}, func(http.ResponseWriter, *http.Request) { t.Error("wrote to one of two records") })
	defer srv.Close()

	cf := cloudflare{client: clientFor(t, srv)}
	err := cf.Update(context.Background(), cloudflareRecordFixture())
	if err == nil {
		t.Fatal("two matching records should be refused")
	}
	var tooMany ErrTooManyRecords
	if !errors.As(err, &tooMany) {
		t.Fatalf("err = %v, want ErrTooManyRecords", err)
	}
	if tooMany.Count != 2 {
		t.Errorf("Count = %d", tooMany.Count)
	}
}

// docs/ddns.md §7: the provider's own error text, not our paraphrase of it.
func TestCloudflareReportsItsOwnRefusal(t *testing.T) {
	rec := &recorder{t: t}
	srv := cloudflareServer(t, rec, nil, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(cloudflareStatus{
			Success: false,
			Errors:  []cloudflareError{{Code: 9103, Message: "Unknown X-Auth-Key or X-Auth-Email"}},
		})
	})
	defer srv.Close()

	cf := cloudflare{client: clientFor(t, srv)}
	err := cf.Update(context.Background(), cloudflareRecordFixture())
	if err == nil {
		t.Fatal("a 200 with success:false is a failure, not a success")
	}
	if !strings.Contains(err.Error(), "Unknown X-Auth-Key") || !strings.Contains(err.Error(), "9103") {
		t.Errorf("err = %v; want the provider's own words", err)
	}
}

// A token scoped to one zone answers the zone lookup with an empty list rather
// than with an authentication failure, so the message has to cover both.
func TestCloudflareSaysWhenTheZoneIsInvisible(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(cloudflareZonesResp{
			cloudflareStatus: cloudflareStatus{Success: true},
		})
	}))
	defer srv.Close()

	cf := cloudflare{client: clientFor(t, srv)}
	err := cf.Update(context.Background(), cloudflareRecordFixture())
	if err == nil || !strings.Contains(err.Error(), "example.net") {
		t.Fatalf("err = %v; want the zone named", err)
	}
	if !strings.Contains(err.Error(), "token") {
		t.Errorf("err = %v; want the scoped-token reading offered too", err)
	}
}

// A record that did not ask for a TTL gets Cloudflare's "auto", which is 1.
// Sending 0 is rejected by the API.
func TestCloudflareDefaultsToAutoTTL(t *testing.T) {
	rec := &recorder{t: t}
	srv := cloudflareServer(t, rec, nil, okStatus)
	defer srv.Close()

	r := cloudflareRecordFixture()
	r.TTL = 0
	cf := cloudflare{client: clientFor(t, srv)}
	if err := cf.Update(context.Background(), r); err != nil {
		t.Fatal(err)
	}

	var body cloudflareRecord
	if err := json.Unmarshal([]byte(rec.calls[len(rec.calls)-1].Body), &body); err != nil {
		t.Fatal(err)
	}
	if body.TTL != cloudflareAutoTTL {
		t.Errorf("TTL = %d, want %d", body.TTL, cloudflareAutoTTL)
	}
}
