package noderun

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/admission"
	"github.com/xallor-ai/xallor-remote/internal/delegation"
	"github.com/xallor-ai/xallor-remote/internal/identity"
	"github.com/xallor-ai/xallor-remote/internal/principal"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
	"github.com/xallor-ai/xallor-remote/internal/relay"
	"github.com/xallor-ai/xallor-remote/internal/storage"
)

// Use a test-only CA with real Relay identity/lease services and Runtime process
// execution. Neither certificate verification nor execution authorization is skipped.
func TestTLS_should_executeInRuntime_whenRelayCertificateIsTrusted(t *testing.T) {
	s, err := storage.OpenSQLStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	key, err := s.ExecutionKey()
	if err != nil {
		t.Fatal(err)
	}
	accounts, permissions := principal.NewService(s), access.NewService(s)
	hub := relay.NewHub(s, accounts, permissions, delegation.NewAuthority(key, s, accounts, permissions), admission.New(admission.Limits{PerDevice: protocol.MaxDeviceRequests}))
	server := httptest.NewTLSServer(relay.Handler(hub, nil))
	defer server.Close()
	// All tests in this package are serial. Restore the default before leaving.
	previous := http.DefaultTransport
	http.DefaultTransport = server.Client().Transport
	defer func() { http.DefaultTransport = previous }()
	st, err := identity.LoadFrom(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	url := "wss" + server.URL[5:]
	if err := st.SetConfig(url, t.TempDir(), "auto"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetInbound(true); err != nil {
		t.Fatal(err)
	}
	id, _, grant, _, _, _, _ := st.Snapshot()
	d := New(st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	connected := make(chan error, 1)
	go func() { connected <- d.connectDevice() }()
	defer func() { d.Stop(); <-connected; d.waitLocalExecs(); d.handoffs.Close(); d.jobs.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	for {
		d.mu.Lock()
		ready := d.device != nil
		d.mu.Unlock()
		if ready {
			break
		}
		select {
		case err := <-connected:
			connected <- err
			t.Fatal("device connection failed", err)
		case <-ctx.Done():
			t.Fatal("device did not connect")
		case <-time.After(10 * time.Millisecond):
		}
	}
	c, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	if err := wsjson.Write(ctx, c, protocol.Message{Type: protocol.TypeHelloClient, TargetID: id, Grant: grant}); err != nil {
		t.Fatal(err)
	}
	var ack protocol.Message
	if err := wsjson.Read(ctx, c, &ack); err != nil || ack.Type != protocol.TypeHelloOK {
		t.Fatal(ack, err)
	}
	if err := wsjson.Write(ctx, c, protocol.Message{Type: protocol.TypeInvoke, ExecID: "tls-exec", Op: protocol.OpExec, Payload: json.RawMessage(`{"script":{"text":"echo xr-tls-executed"},"timeout_ms":30000}`)}); err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	for {
		var message protocol.Message
		if err := wsjson.Read(ctx, c, &message); err != nil {
			t.Fatal(err)
		}
		switch message.Type {
		case protocol.TypeStdout:
			output.WriteString(message.Data)
		case protocol.TypeInvokeNack, protocol.TypeError:
			t.Fatal(message)
		case protocol.TypeExit:
			if message.ExitCode == nil || *message.ExitCode != 0 || !strings.Contains(output.String(), "xr-tls-executed") {
				t.Fatal(message, output.String())
			}
			return
		}
	}
}
