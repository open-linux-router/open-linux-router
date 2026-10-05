package qos

import (
	"strings"
	"testing"
)

func TestPolicyRoundTrip(t *testing.T) {
	c, err := Parse([]byte(`{"enabled":false,"devices":[{"mac":"AA-BB-CC-DD-EE-FF","priority":"high","upload_mbps":5,"download_mbps":20}]}`))
	if err != nil {
		t.Fatal(err)
	}
	d := c.Device("aa:bb:cc:dd:ee:ff")
	if d.Priority != High || d.UploadMbps != 5 || d.DownloadMbps != 20 {
		t.Fatalf("wrong policy: %+v", d)
	}
	c.SetDevice(Device{MAC: d.MAC, Priority: Normal})
	if len(c.Devices) != 0 {
		t.Fatal("reset should remove redundant override")
	}
}
func TestInvalidPolicies(t *testing.T) {
	for _, raw := range []string{
		`{"enabled":true,"download_mbps":100,"upload_mbps":20}`, `{"enabled":false,"devices":[{"mac":"nope"}]}`,
		`{"devices":[{"mac":"aa:bb:cc:dd:ee:ff","priority":"urgent"}]}`,
		`{"devices":[{"mac":"aa:bb:cc:dd:ee:ff","upload_mbps":-2}]}`,
		`{"devices":[{"mac":"aa:bb:cc:dd:ee:ff"},{"mac":"AA-BB-CC-DD-EE-FF"}]}`,
		`{"enabled":false,"typo":true}`,
	} {
		if _, err := Parse([]byte(raw)); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	if _, err := Parse([]byte(`{} {}`)); err == nil || !strings.Contains(err.Error(), "extra JSON") {
		t.Errorf("trailing JSON: %v", err)
	}
}
