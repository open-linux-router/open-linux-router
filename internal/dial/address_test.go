package dial

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

func testLinks() StaticLinks {
	return StaticLinks{
		"eth0": {Adopted: true, Up: true, Prefixes: []netip.Prefix{
			netip.MustParsePrefix("198.51.100.7/24"),
		}},
		"eth1": {Adopted: true, Up: true, Prefixes: []netip.Prefix{
			netip.MustParsePrefix("192.168.1.2/24"),
		}},
		"eth2": {Adopted: false, Up: true, Prefixes: []netip.Prefix{
			netip.MustParsePrefix("203.0.113.4/24"),
		}},
		"eth3": {Adopted: true, Up: true},
	}
}

func TestInterfaceSourceReadsTheKernelsAddress(t *testing.T) {
	d := Discoverer{Links: testLinks()}
	rec := Record{Name: "home.example.net", Source: SourceInterface, Interface: "eth0"}

	addr, err := d.Address(context.Background(), rec)
	if err != nil {
		t.Fatalf("Address: %v", err)
	}
	if addr.String() != "198.51.100.7" {
		t.Errorf("addr = %s", addr)
	}
}

// Releasing an interface after the record was stored has to stop it being read,
// not merely stop it validating — otherwise the publisher keeps reading an
// interface nobody handed over (design.md §3.4).
func TestInterfaceSourceRefusesAnUnadoptedInterface(t *testing.T) {
	d := Discoverer{Links: testLinks()}
	rec := Record{Name: "home.example.net", Source: SourceInterface, Interface: "eth2"}

	if _, err := d.Address(context.Background(), rec); err == nil {
		t.Fatal("reading an unadopted interface should be refused")
	}
}

func TestInterfaceSourceSaysWhenThereIsNoAddressYet(t *testing.T) {
	d := Discoverer{Links: testLinks()}
	rec := Record{Name: "home.example.net", Source: SourceInterface, Interface: "eth3"}

	_, err := d.Address(context.Background(), rec)
	if err == nil || !strings.Contains(err.Error(), "no IPv4 address") {
		t.Fatalf("err = %v; want the missing address named", err)
	}
}

// The private address is *returned*, not swapped for a reflector reading.
// design.md §5.6 forbids that inference; the validator warns instead.
func TestInterfaceSourceReturnsAPrivateAddressRatherThanGuessing(t *testing.T) {
	d := Discoverer{Links: testLinks()}
	rec := Record{Name: "home.example.net", Source: SourceInterface, Interface: "eth1"}

	addr, err := d.Address(context.Background(), rec)
	if err != nil {
		t.Fatalf("Address: %v", err)
	}
	if addr.String() != "192.168.1.2" {
		t.Errorf("addr = %s; a private address is the honest reading of this interface", addr)
	}
}

func TestReflectorReadsTheAddressBack(t *testing.T) {
	for name, body := range map[string]string{
		"bare":        "203.0.113.9",
		"newline":     "203.0.113.9\n",
		"json":        `{"ip":"203.0.113.9"}`,
		"html":        "<html><body>Your IP is 203.0.113.9</body></html>",
		"with a port": "203.0.113.9:41234",
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := r.Header.Get("User-Agent"); got != "open-linux-router" {
					t.Errorf("User-Agent = %q; a free reflector's operator deserves to know who is asking", got)
				}
				_, _ = w.Write([]byte(body))
			}))
			defer srv.Close()

			d := Discoverer{Client: srv.Client()}
			rec := Record{Name: "home.example.net", Source: SourceReflector, ReflectorURL: srv.URL}

			addr, err := d.Address(context.Background(), rec)
			if err != nil {
				t.Fatalf("Address: %v", err)
			}
			if addr.String() != "203.0.113.9" {
				t.Errorf("addr = %s", addr)
			}
		})
	}
}

// An error page that happens to contain an address must not be published.
func TestReflectorIgnoresAddressesThatCannotBeAnAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("could not reach 127.0.0.1 or 0.0.0.0"))
	}))
	defer srv.Close()

	d := Discoverer{Client: srv.Client()}
	rec := Record{Name: "home.example.net", Source: SourceReflector, ReflectorURL: srv.URL}

	if _, err := d.Address(context.Background(), rec); err == nil {
		t.Fatal("loopback and unspecified addresses are not answers")
	}
}

// docs/ddns.md §7's first row: it failed, and the operator hears so. Keeping
// the old address and reporting success is the one thing this must not do.
func TestReflectorReportsARefusal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	d := Discoverer{Client: srv.Client()}
	rec := Record{Name: "home.example.net", Source: SourceReflector, ReflectorURL: srv.URL}

	_, err := d.Address(context.Background(), rec)
	if err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatalf("err = %v; want the reflector's status", err)
	}
}

// The diagnosis nothing else in the product can offer (docs/ddns.md §3.2).
func TestCGNATIsRecognised(t *testing.T) {
	for addr, want := range map[string]bool{
		"100.64.0.1":     true,
		"100.100.100.1":  true,
		"100.127.255.":   false, // not an address at all
		"100.63.255.255": false,
		"100.128.0.0":    false,
		"203.0.113.9":    false,
		"192.168.1.1":    false,
	} {
		parsed, err := netip.ParseAddr(addr)
		if err != nil {
			if want {
				t.Errorf("%q does not parse but the test expects it to be CGNAT", addr)
			}
			continue
		}
		if got := IsCGNAT(parsed); got != want {
			t.Errorf("IsCGNAT(%s) = %v, want %v", addr, got, want)
		}
	}
}

// A hand-edited config with no source must not have one picked for it.
func TestNoSourceIsRefusedRatherThanGuessed(t *testing.T) {
	d := Discoverer{Links: testLinks()}
	rec := Record{Name: "home.example.net", Interface: "eth0", ReflectorURL: "https://example.invalid"}

	if _, err := d.Address(context.Background(), rec); err == nil {
		t.Fatal("a record with no source should be refused, not resolved from whichever field is set")
	}
}
