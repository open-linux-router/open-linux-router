package daemon

import (
	"crypto/x509"
	"encoding/pem"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestTCPManagementSourceRestrictions(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, tc := range []struct {
		networks string
		code     int
	}{
		{"127.0.0.0/8", 200}, {"192.168.1.0/24", 403},
	} {
		tcp, _, err := tcpListener(options{listen: "127.0.0.1:0", adminNetworks: tc.networks}, newApplier(t, claimed), stub(), logger)
		if err != nil {
			t.Fatal(err)
		}
		go tcp.server.Serve(tcp.listener)
		req, _ := http.NewRequest("GET", "http://"+tcp.listener.Addr().String()+"/api/modules", nil)
		req.Header.Set("X-Forwarded-For", "192.168.1.10")
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		tcp.server.Close()
		if response.StatusCode != tc.code {
			t.Fatal(tc.networks, response.StatusCode)
		}
	}
	if _, _, err := tcpListener(options{listen: "127.0.0.1:0", adminNetworks: "invalid"}, newApplier(t, claimed), stub(), logger); err == nil {
		t.Fatal("invalid management prefix accepted")
	}
}

func TestTCPHTTPSAndInvalidTLSConfiguration(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	certificateServer := httptest.NewTLSServer(stub())
	defer certificateServer.Close()
	certificate := certificateServer.TLS.Certificates[0]
	key, err := x509.MarshalPKCS8PrivateKey(certificate.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	certPath, keyPath := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Certificate[0]}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, opts := range []options{{tlsCert: certPath}, {tlsKey: keyPath}, {tlsCert: "missing", tlsKey: keyPath}} {
		opts.listen = "127.0.0.1:0"
		if _, _, err := tcpListener(opts, newApplier(t, claimed), stub(), logger); err == nil {
			t.Fatal("invalid TLS configuration accepted")
		}
	}
	tcp, _, err := tcpListener(options{listen: "127.0.0.1:0", tlsCert: certPath, tlsKey: keyPath}, newApplier(t, claimed), stub(), logger)
	if err != nil {
		t.Fatal(err)
	}
	defer tcp.server.Close()
	go tcp.server.Serve(tcp.listener)
	url := "https://" + tcp.listener.Addr().String() + "/api/modules"
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Origin", "https://"+tcp.listener.Addr().String())
	response, err := certificateServer.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 || response.TLS == nil {
		t.Fatal("HTTPS did not serve the API", response.StatusCode)
	}
}
