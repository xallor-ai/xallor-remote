package relay

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/xallor-ai/xallor-remote/internal/admission"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Load PEM files through the same HTTP server as the CLI. Untrusted clients and
// mismatched hostnames fail; trusted clients can probe health and exchange WSS.
func TestTLS_should_verifyCertificatesAndRoute_whenPEMListenerStarts(t *testing.T) {
	fixture := httptest.NewTLSServer(http.NotFoundHandler())
	defer fixture.Close()
	cert := fixture.TLS.Certificates[0]
	key, err := x509.MarshalPKCS8PrivateKey(cert.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	config := Transport{CertFile: filepath.Join(dir, "cert.pem"), KeyFile: filepath.Join(dir, "key.pem")}
	if err := os.WriteFile(config.CertFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]}), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.KeyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}), 0600); err != nil {
		t.Fatal(err)
	}
	hub := NewHub(newMemStore(), nil, nil, nil, admission.New(admission.Limits{PerDevice: protocol.MaxDeviceRequests}))
	srv, err := config.server("127.0.0.1:0", hub, nil, Quota{})
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- srv.ServeTLS(listener, config.CertFile, config.KeyFile) }()
	defer func() { _ = srv.Close(); <-done }()
	url := "https://" + listener.Addr().String()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	untrusted := &http.Client{Timeout: time.Second * 3}
	if response, err := untrusted.Get(url + "/health"); err == nil {
		response.Body.Close()
		t.Fatal("untrusted cert accepted")
	}
	client := fixture.Client()
	response, err := client.Get(url + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal(response.Status)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	transport := client.Transport.(*http.Transport).Clone()
	transport.TLSClientConfig = transport.TLSClientConfig.Clone()
	transport.TLSClientConfig.ServerName = "wrong.example"
	defer transport.CloseIdleConnections()
	if response, err := (&http.Client{Transport: transport, Timeout: 3 * time.Second}).Get(url + "/health"); err == nil {
		response.Body.Close()
		t.Fatal("wrong hostname accepted")
	}
	c, _, err := websocket.Dial(ctx, "wss"+url[5:], &websocket.DialOptions{HTTPClient: client})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	if err := wsjson.Write(ctx, c, protocol.Message{Type: protocol.TypeHelloDevice, DeviceID: "tls-device", Secret: "test-secret"}); err != nil {
		t.Fatal(err)
	}
	var ack protocol.Message
	if err := wsjson.Read(ctx, c, &ack); err != nil || ack.Type != protocol.TypeHelloOK {
		t.Fatal(ack, err)
	}
	if err := Serve("127.0.0.1:0", hub, nil, Quota{}, Transport{CertFile: filepath.Join(dir, "missing"), KeyFile: config.KeyFile}); err == nil {
		t.Fatal("missing certificate accepted")
	}
}
