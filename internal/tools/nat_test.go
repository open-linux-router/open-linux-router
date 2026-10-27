package tools

import (
	"context"
	"encoding/binary"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

func TestNATRoute(t *testing.T) {
	h := &HTTP{RunNAT: func(context.Context) (NATResult, error) {
		return NATResult{Server: "test", Mapped: "203.0.113.1:1234", Mapping: "not tested", Filtering: "not tested"}, nil
	}}
	w := httptest.NewRecorder()
	core.RouteTable(h.Routes()).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/nat", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"mapped":"203.0.113.1:1234"`) {
		t.Fatalf("%d: %s", w.Code, w.Body.String())
	}
}

func TestParseSTUN(t *testing.T) {
	var tx [12]byte
	copy(tx[:], "transaction!")
	b := make([]byte, 20, 48)
	binary.BigEndian.PutUint16(b, 0x101)
	binary.BigEndian.PutUint32(b[4:], stunMagic)
	copy(b[8:], tx[:])
	mapped := []byte{0, 1, 0, 0, 0, 0, 0, 0}
	binary.BigEndian.PutUint16(mapped[2:], 1234^uint16(stunMagic>>16))
	ip := net.ParseIP("203.0.113.1").To4()
	for i := range ip {
		mapped[4+i] = ip[i] ^ byte((stunMagic>>(24-8*i))&0xff)
	}
	b = append(b, 0, 0x20, 0, 8)
	b = append(b, mapped...)
	binary.BigEndian.PutUint16(b[2:], uint16(len(b)-20))
	result, ok := parseSTUN(b, tx)
	if !ok || result.mapped != "203.0.113.1:1234" {
		t.Fatalf("%+v %v", result, ok)
	}
	b[8]++
	if _, ok := parseSTUN(b, tx); ok {
		t.Fatal("accepted mismatched transaction")
	}
}

func TestAlternateMustBePublic(t *testing.T) {
	for _, host := range []string{"10.0.0.1", "127.0.0.1", "100.64.0.1", "198.18.0.1", "203.0.113.1", "169.254.1.1", "::1"} {
		if publicIPv4(host) {
			t.Fatal(host)
		}
	}
	if !publicIPv4("212.227.67.34") {
		t.Fatal("rejected public alternate")
	}
}

func TestAlternateUsable(t *testing.T) {
	cases := []struct {
		name  string
		probe stunProbe
		want  bool
	}{
		{"no alternate", stunProbe{target: "212.227.67.33:3478"}, false},
		{"same address, other port", stunProbe{target: "137.74.112.113:3478", other: "137.74.112.113:3479"}, false},
		{"private alternate", stunProbe{target: "212.227.67.33:3478", other: "10.0.0.1:3479"}, false},
		{"port zero", stunProbe{target: "212.227.67.33:3478", other: "212.227.67.34:0"}, false},
		{"different address", stunProbe{target: "212.227.67.33:3478", other: "212.227.67.34:3479"}, true},
	}
	for _, c := range cases {
		if got := alternateUsable(c.probe); got != c.want {
			t.Fatalf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestClassifyPrefersServerThatCanClassify(t *testing.T) {
	probes := []stunProbe{
		{server: "plain:3478", target: "198.51.100.10:3478", mapped: "203.0.113.7:5000"},
		{server: "capable:3478", target: "212.227.67.33:3478", mapped: "203.0.113.7:5001", other: "212.227.67.34:3479"},
	}
	classify := func(_ context.Context, probe stunProbe) (string, string) {
		if probe.server != "capable:3478" {
			t.Fatalf("classified %s, which offers no alternate address", probe.server)
		}
		return "endpoint-independent", "address-dependent"
	}
	result, err := classifyProbes(context.Background(), probes, classify)
	if err != nil {
		t.Fatal(err)
	}
	if result.Server != "capable:3478" || !result.Alternate || result.Mapping != "endpoint-independent" || result.Filtering != "address-dependent" {
		t.Fatalf("%+v", result)
	}
}

func TestClassifyPrefersCapableServerWhenNeitherClassifies(t *testing.T) {
	probes := []stunProbe{
		{server: "plain:3478", target: "198.51.100.10:3478", mapped: "203.0.113.7:5000"},
		{server: "capable:3478", target: "212.227.67.33:3478", mapped: "203.0.113.7:5001", other: "212.227.67.34:3479"},
	}
	classify := func(context.Context, stunProbe) (string, string) { return natNotTested, natNotTested }
	result, err := classifyProbes(context.Background(), probes, classify)
	if err != nil {
		t.Fatal(err)
	}
	if result.Server != "capable:3478" || !result.Alternate {
		t.Fatalf("%+v", result)
	}
}

func TestClassifyKeepsMappingWithoutAlternate(t *testing.T) {
	probes := []stunProbe{{server: "plain:3478", target: "198.51.100.10:3478", mapped: "203.0.113.7:5000"}}
	result, err := classifyProbes(context.Background(), probes, func(context.Context, stunProbe) (string, string) {
		t.Fatal("classified a server with no alternate address")
		return "", ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Mapping != natNotTested || result.Filtering != natNotTested || result.Alternate || result.Mapped != "203.0.113.7:5000" {
		t.Fatalf("%+v", result)
	}
}

func TestClassifyRequiresAnAnswer(t *testing.T) {
	if _, err := classifyProbes(context.Background(), []stunProbe{{server: "silent:3478"}}, nil); err == nil {
		t.Fatal("accepted a round with no mapping")
	}
}
