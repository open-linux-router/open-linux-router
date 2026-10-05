package ingress

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestUpstreamHeaderFlags(t *testing.T) {
	var flags upstreamFlags
	cmd := &cobra.Command{}
	flags.register(cmd)
	if err := cmd.ParseFlags([]string{
		"--header", "Host: 172.16.1.163",
		"--header", "X-Empty:",
		"--header", "X-Space:  padded ",
		"--remove-header", "X-Internal",
	}); err != nil {
		t.Fatal(err)
	}
	var upstream Upstream
	flags.apply(&upstream, cmd)
	want := []RequestHeader{
		{Name: "Host", Value: "172.16.1.163"},
		{Name: "X-Empty"},
		{Name: "X-Space", Value: " padded "},
		{Name: "X-Internal", Remove: true},
	}
	if len(upstream.RequestHeaders) != len(want) {
		t.Fatalf("headers = %+v", upstream.RequestHeaders)
	}
	for i := range want {
		if upstream.RequestHeaders[i] != want[i] {
			t.Errorf("header %d = %+v, want %+v", i, upstream.RequestHeaders[i], want[i])
		}
	}
}
