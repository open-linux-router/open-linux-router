package socksout

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		ok          bool
	}{
		{"plain", `{"enabled":true,"proxy":"socks5://192.0.2.10:1080"}`, true},
		{"ipv6", `{"enabled":true,"proxy":"socks5://[2001:db8::1]:1080"}`, true},
		{"authenticated", `{"enabled":true,"proxy":"socks5://user:pass@[2001:db8::1]:1080"}`, false},
		{"disabled", `{}`, true},
		{"missing", `{"enabled":true}`, false},
		{"other protocol", `{"enabled":true,"proxy":"http://192.0.2.10:1080"}`, false},
		{"hostname", `{"enabled":true,"proxy":"socks5://example.com:1080"}`, false},
		{"loopback", `{"enabled":true,"proxy":"socks5://127.0.0.1:1080"}`, false},
		{"port", `{"enabled":true,"proxy":"socks5://192.0.2.10:0"}`, false},
		{"empty password", `{"enabled":true,"proxy":"socks5://user:@192.0.2.10:1080"}`, false},
		{"query", `{"enabled":true,"proxy":"socks5://192.0.2.10:1080?x=1"}`, false},
		{"unknown field", `{"enabled":true,"proxy":"socks5://192.0.2.10:1080","other":true}`, false},
		{"trailing", `{"enabled":true,"proxy":"socks5://192.0.2.10:1080"} {}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.input))
			if (err == nil) != tc.ok {
				t.Fatalf("Parse: %v, want valid %t", err, tc.ok)
			}
			if err != nil && strings.Contains(err.Error(), "pass") {
				t.Fatalf("error disclosed credentials: %v", err)
			}
		})
	}
}
