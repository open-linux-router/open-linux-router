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
