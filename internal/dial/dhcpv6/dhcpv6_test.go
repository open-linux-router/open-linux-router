package dhcpv6

import (
	"bytes"
	"context"
	"encoding/hex"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"
)

// A Solicit as the wire has it, built by hand from RFC 8415's layouts so the
// encoder is checked against the RFC and not against itself.
func TestSolicitEncodesToTheRFCLayout(t *testing.T) {
	mac, _ := net.ParseMAC("52:54:00:12:34:56")
	m := Message{
		Type: Solicit, XID: [3]byte{0xaa, 0xbb, 0xcc},
		ClientID: DUIDLL(mac), RequestDNS: true, RapidCommit: true,
		IAPD: &IAPD{IAID: 1, Prefixes: []IAPrefix{{Prefix: netip.PrefixFrom(netip.IPv6Unspecified(), 60)}}},
	}
	want := strings.Join([]string{
		"01aabbcc",                               // Solicit, xid
		"0001000a" + "00030001" + "525400123456", // client ID: DUID-LL, Ethernet, MAC
		"00080002" + "0000",                      // elapsed time 0
		"00060002" + "0017",                      // ORO: DNS servers
		"000e0000",                               // rapid commit
		"0019" + "0029" + "00000001" + "00000000" + "00000000" + // IA_PD, IAID 1, T1 T2 0
			"001a0019" + "00000000" + "00000000" + "3c" + strings.Repeat("00", 16), // hint ::/60
	}, "")
	if got := hex.EncodeToString(m.Encode()); got != want {
		t.Errorf("encoded\n%s\nwant\n%s", got, want)
	}
}

func TestAReplyDecodesToALease(t *testing.T) {
	reply := Message{
		Type: Reply, XID: [3]byte{1, 2, 3}, ClientID: []byte{0, 3, 0, 1, 1, 2, 3, 4, 5, 6},
		ServerID: []byte{0, 1, 9, 9},
		IAPD: &IAPD{IAID: 1, T1: time.Hour, T2: 2 * time.Hour, Prefixes: []IAPrefix{{
			Prefix:    netip.MustParsePrefix("2408:8000:1234:5600::/56"),
			Preferred: 3 * time.Hour, Valid: 4 * time.Hour,
		}}},
	}
	m, err := Decode(reply.Encode())
	if err != nil {
		t.Fatal(err)
	}
	if !usable(m, 1) || usable(m, 2) {
		t.Fatalf("usable(1)=%v usable(2)=%v", usable(m, 1), usable(m, 2))
	}
	l := (&Client{Now: func() time.Time { return time.Unix(1000, 0) }}).lease(m)
	if l.Prefix.String() != "2408:8000:1234:5600::/56" || l.T1 != time.Hour || l.T2 != 2*time.Hour ||
		l.Valid != 4*time.Hour || !bytes.Equal(l.ServerID, reply.ServerID) {
		t.Errorf("lease = %+v", l)
	}
}

// T1 and T2 of zero are the client's to choose: half and eight tenths.
func TestZeroTimersAreDerivedFromThePreferredLifetime(t *testing.T) {
	m := Message{IAPD: &IAPD{Prefixes: []IAPrefix{{
		Prefix: netip.MustParsePrefix("2001:db8::/56"), Preferred: 10 * time.Hour, Valid: 20 * time.Hour}}}}
	l := (&Client{}).lease(m)
	if l.T1 != 5*time.Hour || l.T2 != 8*time.Hour {
		t.Errorf("T1 %s T2 %s", l.T1, l.T2)
	}
}

func TestNoPrefixAvailIsSaidInTheISPsTerms(t *testing.T) {
	m := Message{Type: Reply, IAPD: &IAPD{IAID: 1, Status: StatusNoPrefixAvail, StatusMessage: "none left"}}
	decoded, err := Decode(m.Encode())
	if err != nil {
		t.Fatal(err)
	}
	// Encode does not write an IA's status; build the decoded form directly.
	decoded.IAPD = m.IAPD
	if err := refusal(decoded); err == nil || !strings.Contains(err.Error(), "NoPrefixAvail") {
		t.Errorf("refusal = %v", err)
	}
}

// fakeServer answers on a Conn the way an ISP's server would, by message type.
type fakeServer struct {
	t       *testing.T
	answer  func(Message) *Message
	pending [][]byte
	sent    []MessageType
}

func (s *fakeServer) Send(_ context.Context, b []byte) error {
	m, err := Decode(b)
	if err != nil {
		s.t.Fatalf("client sent something undecodable: %v", err)
	}
	s.sent = append(s.sent, m.Type)
	if a := s.answer(m); a != nil {
		a.XID, a.ClientID = m.XID, m.ClientID
		s.pending = append(s.pending, a.Encode())
	}
	return nil
}

func (s *fakeServer) Receive(context.Context, time.Time) ([]byte, error) {
	if len(s.pending) == 0 {
		return nil, ErrTimeout
	}
	b := s.pending[0]
	s.pending = s.pending[1:]
	return b, nil
}

func delegated() *IAPD {
	return &IAPD{IAID: 1, T1: time.Hour, T2: 2 * time.Hour, Prefixes: []IAPrefix{{
		Prefix: netip.MustParsePrefix("2408:8000:1234:5600::/60"), Preferred: 3 * time.Hour, Valid: 4 * time.Hour}}}
}

func TestAcquireRunsSolicitAdvertiseRequestReply(t *testing.T) {
	srv := &fakeServer{t: t, answer: func(m Message) *Message {
		switch m.Type {
		case Solicit:
			return &Message{Type: Advertise, ServerID: []byte{0, 1}, IAPD: delegated()}
		case Request:
			if m.IAPD == nil || len(m.IAPD.Prefixes) != 1 || string(m.ServerID) != string([]byte{0, 1}) {
				t.Errorf("request did not carry the advertised prefix and server: %+v", m)
			}
			return &Message{Type: Reply, ServerID: []byte{0, 1}, IAPD: delegated()}
		}
		return nil
	}}
	c := &Client{Conn: srv, ClientID: []byte{0, 3, 0, 1, 1, 2, 3, 4, 5, 6}, IAID: 1, Hint: 60}
	l, err := c.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if l.Prefix.String() != "2408:8000:1234:5600::/60" {
		t.Errorf("prefix = %s", l.Prefix)
	}
	if len(srv.sent) != 2 || srv.sent[0] != Solicit || srv.sent[1] != Request {
		t.Errorf("sent %v", srv.sent)
	}
}

// Rapid Commit: the server answers the Solicit with a Reply and there is no
// Request at all.
func TestAcquireTakesARapidCommitReply(t *testing.T) {
	srv := &fakeServer{t: t, answer: func(m Message) *Message {
		return &Message{Type: Reply, RapidCommit: true, ServerID: []byte{0, 1}, IAPD: delegated()}
	}}
	c := &Client{Conn: srv, ClientID: []byte{0, 3}, IAID: 1}
	if _, err := c.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(srv.sent) != 1 {
		t.Errorf("sent %v, want the Solicit alone", srv.sent)
	}
}

// A reply to somebody else's transaction is not ours, however usable.
func TestAnAnswerToAnotherTransactionIsIgnored(t *testing.T) {
	calls := 0
	srv := &fakeServer{t: t}
	srv.answer = func(m Message) *Message {
		calls++
		if calls == 1 {
			// Wrong XID: queued raw so the fake cannot correct it.
			stray := Message{Type: Reply, RapidCommit: true, XID: [3]byte{9, 9, 9}, ClientID: m.ClientID,
				IAPD: delegated()}
			srv.pending = append(srv.pending, stray.Encode())
			return nil
		}
		return &Message{Type: Reply, RapidCommit: true, ServerID: []byte{0, 1}, IAPD: delegated()}
	}
	c := &Client{Conn: srv, ClientID: []byte{0, 3}, IAID: 1}
	// The first Solicit's wait is a second of real time; the ctx bounds it.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := c.Acquire(ctx); err != nil {
		t.Fatal(err)
	}
	if len(srv.sent) != 2 {
		t.Errorf("sent %v; the stray reply should have forced a retransmission", srv.sent)
	}
}
