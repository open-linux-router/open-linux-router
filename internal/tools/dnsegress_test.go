package tools

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"golang.org/x/net/dns/dnsmessage"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

func TestDNSEgressAsksBothSources(t *testing.T) {
	server := whoamiServer(t, netip.MustParseAddr("203.0.113.9"))
	h := &HTTP{DNSLegs: func() ([]DNSLeg, error) {
		return []DNSLeg{{Name: "This router", Server: server, Kind: "router"}}, nil
	}}
	w := httptest.NewRecorder()
	core.RouteTable(h.Routes()).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/dns-exit", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var report DNSExitReport
	if err := json.Unmarshal(w.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Legs) != 1 {
		t.Fatalf("got %d legs", len(report.Legs))
	}
	leg := report.Legs[0]
	if leg.Error != "" || len(leg.Answers) != 2 {
		t.Fatalf("leg: %+v", leg)
	}
	for _, answer := range leg.Answers {
		if answer.IP != "203.0.113.9" {
			t.Fatalf("%s: %s", answer.Source, answer.IP)
		}
	}
}

func TestDNSEgressReportsAResolverThatDidNotAnswer(t *testing.T) {
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := conn.LocalAddr().String()
	_ = conn.Close()

	report, err := measureDNSEgress(context.Background(), func() ([]DNSLeg, error) {
		return []DNSLeg{{Name: "Proxy", Server: server, Kind: "exit"}}, nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	leg := report.Legs[0]
	if leg.Error == "" || len(leg.Answers) != 0 {
		t.Fatalf("leg: %+v", leg)
	}
}

func TestDNSEgressWithoutLegsIsUnavailable(t *testing.T) {
	h := &HTTP{}
	w := httptest.NewRecorder()
	core.RouteTable(h.Routes()).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/dns-exit", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
}

func TestAnswerAddressIgnoresAnotherQuestion(t *testing.T) {
	builder := dnsmessage.NewBuilder(nil, dnsmessage.Header{ID: 7, Response: true})
	builder.StartQuestions()
	builder.Question(dnsmessage.Question{Name: dnsmessage.MustNewName("whoami.akamai.net."), Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET})
	builder.StartAnswers()
	builder.AResource(dnsmessage.ResourceHeader{Name: dnsmessage.MustNewName("whoami.akamai.net."), Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}, dnsmessage.AResource{A: [4]byte{203, 0, 113, 9}})
	message, err := builder.Finish()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := answerAddress(message, 8); err == nil {
		t.Fatal("accepted an answer to another question")
	}
}

// whoamiServer answers every question with addr, so the wire path is tested
// without standing in for a resolver's own logic.
func whoamiServer(t *testing.T, addr netip.Addr) string {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	go func() {
		buf := make([]byte, 512)
		for {
			n, from, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			var parser dnsmessage.Parser
			header, err := parser.Start(buf[:n])
			if err != nil {
				continue
			}
			question, err := parser.Question()
			if err != nil {
				continue
			}
			builder := dnsmessage.NewBuilder(nil, dnsmessage.Header{ID: header.ID, Response: true, RecursionAvailable: true})
			builder.StartQuestions()
			builder.Question(question)
			builder.StartAnswers()
			resource := dnsmessage.ResourceHeader{Name: question.Name, Type: question.Type, Class: dnsmessage.ClassINET}
			switch question.Type {
			case dnsmessage.TypeTXT:
				builder.TXTResource(resource, dnsmessage.TXTResource{TXT: []string{addr.String(), "edns0-client-subnet 0.0.0.0/0"}})
			case dnsmessage.TypeA:
				builder.AResource(resource, dnsmessage.AResource{A: addr.As4()})
			}
			message, err := builder.Finish()
			if err != nil {
				continue
			}
			_, _ = conn.WriteTo(message, from)
		}
	}()
	return conn.LocalAddr().String()
}
