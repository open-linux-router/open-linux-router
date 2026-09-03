package dnsrelay

import (
	"net/netip"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// buildQuery makes a real wire-format query.
func buildQuery(t *testing.T, id uint16, name string, qtype dnsmessage.Type) []byte {
	t.Helper()
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{ID: id, RecursionDesired: true})
	b.EnableCompression()
	if err := b.StartQuestions(); err != nil {
		t.Fatal(err)
	}
	if err := b.Question(dnsmessage.Question{
		Name:  dnsmessage.MustNewName(name),
		Type:  qtype,
		Class: dnsmessage.ClassINET,
	}); err != nil {
		t.Fatal(err)
	}
	msg, err := b.Finish()
	if err != nil {
		t.Fatal(err)
	}
	return msg
}

// answer describes one record for buildResponse.
type answer struct {
	owner string
	ttl   uint32
	a     [4]byte
	aaaa  [16]byte
	cname string
}

// buildResponse makes a real wire-format response, CNAME chains included.
func buildResponse(t *testing.T, id uint16, qname string, qtype dnsmessage.Type, rcode dnsmessage.RCode, answers []answer) []byte {
	t.Helper()
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{
		ID: id, Response: true, RecursionDesired: true, RecursionAvailable: true, RCode: rcode,
	})
	b.EnableCompression()
	if err := b.StartQuestions(); err != nil {
		t.Fatal(err)
	}
	if err := b.Question(dnsmessage.Question{
		Name: dnsmessage.MustNewName(qname), Type: qtype, Class: dnsmessage.ClassINET,
	}); err != nil {
		t.Fatal(err)
	}
	if err := b.StartAnswers(); err != nil {
		t.Fatal(err)
	}
	for _, a := range answers {
		h := dnsmessage.ResourceHeader{
			Name: dnsmessage.MustNewName(a.owner), Class: dnsmessage.ClassINET, TTL: a.ttl,
		}
		var err error
		switch {
		case a.cname != "":
			err = b.CNAMEResource(h, dnsmessage.CNAMEResource{CNAME: dnsmessage.MustNewName(a.cname)})
		case a.aaaa != [16]byte{}:
			err = b.AAAAResource(h, dnsmessage.AAAAResource{AAAA: a.aaaa})
		default:
			err = b.AResource(h, dnsmessage.AResource{A: a.a})
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	msg, err := b.Finish()
	if err != nil {
		t.Fatal(err)
	}
	return msg
}

func TestParseQuestion(t *testing.T) {
	query := buildQuery(t, 0x1234, "WWW.Example.COM.", dnsmessage.TypeA)

	q, header, err := ParseQuestion(query)
	if err != nil {
		t.Fatal(err)
	}
	// Canonicalised, so policy matching does not have to normalise on the hot
	// path and "WWW.Example.COM." and "www.example.com" are one key.
	if q.Name != "www.example.com" {
		t.Errorf("Name = %q, want www.example.com", q.Name)
	}
	if q.TypeName != "A" {
		t.Errorf("TypeName = %q, want A", q.TypeName)
	}
	if header.ID != 0x1234 {
		t.Errorf("ID = %#x", header.ID)
	}
}

func TestParseQuestionRejectsGarbage(t *testing.T) {
	if _, _, err := ParseQuestion([]byte{0x00}); err == nil {
		t.Error("a truncated message parsed successfully")
	}
}

// A client matches a response to its query by ID *and* question section, and
// silently discards anything else — so a blocked name that produced a discarded
// answer would look like a timeout, which is the failure blocking exists to
// avoid.
func TestSynthesizeEchoesTheQuery(t *testing.T) {
	query := buildQuery(t, 0xbeef, "ads.example.com.", dnsmessage.TypeA)

	response, err := Synthesize(query, RespondNXDOMAIN)
	if err != nil {
		t.Fatal(err)
	}

	var p dnsmessage.Parser
	header, err := p.Start(response)
	if err != nil {
		t.Fatal(err)
	}
	if header.ID != 0xbeef {
		t.Errorf("ID = %#x, want 0xbeef", header.ID)
	}
	if !header.Response {
		t.Error("the synthesised message is not marked as a response")
	}
	if header.RCode != dnsmessage.RCodeNameError {
		t.Errorf("RCode = %v, want NXDOMAIN", header.RCode)
	}
	q, err := p.Question()
	if err != nil {
		t.Fatal(err)
	}
	if q.Name.String() != "ads.example.com." {
		t.Errorf("the question was not echoed: %s", q.Name)
	}
}

func TestSynthesizeZero(t *testing.T) {
	t.Run("A answers 0.0.0.0", func(t *testing.T) {
		response, err := Synthesize(buildQuery(t, 1, "ads.example.com.", dnsmessage.TypeA), RespondZero)
		if err != nil {
			t.Fatal(err)
		}
		obs, err := Observe(response)
		if err != nil {
			t.Fatal(err)
		}
		if obs.Rcode != "NOERROR" {
			t.Errorf("Rcode = %s, want NOERROR", obs.Rcode)
		}
		if len(obs.Addrs) != 1 || obs.Addrs[0] != netip.AddrFrom4([4]byte{}) {
			t.Errorf("Addrs = %v, want [0.0.0.0]", obs.Addrs)
		}
	})

	t.Run("AAAA answers ::", func(t *testing.T) {
		response, err := Synthesize(buildQuery(t, 1, "ads.example.com.", dnsmessage.TypeAAAA), RespondZero)
		if err != nil {
			t.Fatal(err)
		}
		obs, err := Observe(response)
		if err != nil {
			t.Fatal(err)
		}
		if len(obs.Addrs) != 1 || obs.Addrs[0] != netip.AddrFrom16([16]byte{}) {
			t.Errorf("Addrs = %v, want [::]", obs.Addrs)
		}
	})

	t.Run("another type is NODATA, not a fabricated address", func(t *testing.T) {
		// Answering an address for a qtype that is not an address would be a
		// malformed reply; NXDOMAIN would contradict the A record we promise.
		response, err := Synthesize(buildQuery(t, 1, "ads.example.com.", dnsmessage.TypeMX), RespondZero)
		if err != nil {
			t.Fatal(err)
		}
		obs, err := Observe(response)
		if err != nil {
			t.Fatal(err)
		}
		if obs.Rcode != "NOERROR" || len(obs.Addrs) != 0 {
			t.Errorf("Rcode = %s, Addrs = %v; want NOERROR with no answers", obs.Rcode, obs.Addrs)
		}
	})
}

// www.example.com commonly answers as a CNAME to a CDN with the A record owned
// by the latter. Billing the device for a hostname it never asked for is the
// naive mistake here.
func TestObserveAttributesToTheQNAMEAndKeepsTheChain(t *testing.T) {
	response := buildResponse(t, 1, "www.example.com.", dnsmessage.TypeA, dnsmessage.RCodeSuccess, []answer{
		{owner: "www.example.com.", ttl: 300, cname: "example.cdn.net."},
		{owner: "example.cdn.net.", ttl: 60, a: [4]byte{93, 184, 216, 34}},
	})

	obs, err := Observe(response)
	if err != nil {
		t.Fatal(err)
	}
	if obs.Name != "www.example.com" {
		t.Errorf("Name = %q, want the QNAME", obs.Name)
	}
	if len(obs.Addrs) != 1 || obs.Addrs[0].String() != "93.184.216.34" {
		t.Errorf("Addrs = %v", obs.Addrs)
	}
	// The tail of the chain is the organisation signal — the reason a device
	// that asked for one name shows up talking to a CDN.
	if len(obs.Chain) != 1 || obs.Chain[0] != "example.cdn.net" {
		t.Errorf("Chain = %v", obs.Chain)
	}
	// The smallest TTL among the address records is when the answer stops being
	// true.
	if obs.TTL != 60*time.Second {
		t.Errorf("TTL = %s, want 60s", obs.TTL)
	}
}

// An unrelated name a server chose to include is not an answer to what was
// asked, and attributing it would put addresses in the map that the device will
// never talk to.
func TestObserveIgnoresRecordsOutsideTheChain(t *testing.T) {
	response := buildResponse(t, 1, "www.example.com.", dnsmessage.TypeA, dnsmessage.RCodeSuccess, []answer{
		{owner: "www.example.com.", ttl: 300, a: [4]byte{1, 2, 3, 4}},
		{owner: "unrelated.example.org.", ttl: 300, a: [4]byte{9, 9, 9, 9}},
	})

	obs, err := Observe(response)
	if err != nil {
		t.Fatal(err)
	}
	if len(obs.Addrs) != 1 || obs.Addrs[0].String() != "1.2.3.4" {
		t.Errorf("Addrs = %v, want just the answer to the question", obs.Addrs)
	}
}

func TestObserveNXDOMAIN(t *testing.T) {
	response := buildResponse(t, 1, "nope.example.com.", dnsmessage.TypeA, dnsmessage.RCodeNameError, nil)
	obs, err := Observe(response)
	if err != nil {
		t.Fatal(err)
	}
	if obs.Rcode != "NXDOMAIN" {
		t.Errorf("Rcode = %q", obs.Rcode)
	}
	if len(obs.Addrs) != 0 {
		t.Errorf("Addrs = %v, want none", obs.Addrs)
	}
}

// Malformed responses are routine, not rare. A parse failure has to cost a
// statistics entry and nothing else — never a panic, and never a hang.
func TestObserveOnMalformedInputFailsQuietly(t *testing.T) {
	good := buildResponse(t, 1, "www.example.com.", dnsmessage.TypeA, dnsmessage.RCodeSuccess, []answer{
		{owner: "www.example.com.", ttl: 300, a: [4]byte{1, 2, 3, 4}},
	})

	// Every truncation of a real message. Any of these that hung or panicked
	// would do so in the path of every name the network resolves.
	for i := range good {
		func() {
			defer func() {
				if p := recover(); p != nil {
					t.Errorf("Observe panicked on a %d-byte prefix: %v", i, p)
				}
			}()
			_, _ = Observe(good[:i])
		}()
	}

	// A compression pointer that points at itself: the classic parser hang.
	loop := []byte{
		0x00, 0x01, 0x81, 0x80, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0xc0, 0x0c, // a name that is a pointer to itself
		0x00, 0x01, 0x00, 0x01,
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() { _ = recover() }()
		_, _ = Observe(loop)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Observe hung on a self-referential compression pointer")
	}
}

func TestTypeName(t *testing.T) {
	// Our own table, because dnsmessage.Type.String() returns "TypeA" — fine in
	// a Go error, wrong in a column headed TYPE.
	if got := TypeName(dnsmessage.TypeA); got != "A" {
		t.Errorf("TypeName(A) = %q", got)
	}
	// HTTPS is now a large share of what a browser asks for and dnsmessage does
	// not know it.
	if got := TypeName(65); got != "HTTPS" {
		t.Errorf("TypeName(65) = %q, want HTTPS", got)
	}
	if got := TypeName(999); got != "TYPE999" {
		t.Errorf("TypeName(999) = %q", got)
	}
}

func TestCanonicalNameKeepsTheRoot(t *testing.T) {
	// "." is a legitimate QNAME for an NS query; trimming it to the empty
	// string would read as "no name" everywhere downstream.
	if got := CanonicalName("."); got != "." {
		t.Errorf("CanonicalName(\".\") = %q", got)
	}
	if got := CanonicalName("Example.COM."); got != "example.com" {
		t.Errorf("CanonicalName = %q", got)
	}
}
