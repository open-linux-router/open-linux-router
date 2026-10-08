//go:build linux

package tools

import "testing"

func TestParseForwardRun(t *testing.T) {
	data := []byte(`{"intervals":[{"sum":{"bits_per_second":8000000}}],"end":{"sum_received":{"bits_per_second":8000000,"packets":1000,"lost_packets":10,"seconds":5}}}`)
	run, err := parseForwardRun("UDP", data, true)
	if err != nil {
		t.Fatal(err)
	}
	if run.Mbps != 8 || run.PPS != 198 || run.Loss != 1 || len(run.Samples) != 1 {
		t.Fatalf("run = %+v", run)
	}
	if _, err := parseForwardRun("TCP", []byte(`{"error":"failed"}`), false); err == nil {
		t.Fatal("accepted failed test")
	}
}
