package storage

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/account"
	"github.com/xallor-ai/xallor-remote/internal/admission"
	"github.com/xallor-ai/xallor-remote/internal/principal"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
	"github.com/xallor-ai/xallor-remote/internal/relay"
)

// Real TLS sockets and the real SQL pool cross expiry during authentication or
// per-invoke verification. The expired request cannot reach the target device;
// a subsequent authorized request demonstrates that routing remains usable.
func TestPrincipalWire_should_denyExpiredCaller_whenDatabaseWaitCrossesDeadline(t *testing.T) {
	for _, operation := range []string{"hello", "invoke"} {
		t.Run(operation, func(t *testing.T) {
			s := openIdentityTestStore(t)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			a, err := account.New(s).CreatePrincipal(ctx, account.CreatePrincipalRequest{Name: "expiry", Kind: "user", TTL: time.Hour})
			if err != nil {
				t.Fatal(err)
			}
			// Keep the initial handshake and later verification bound to this same
			// expiry, so rejection cannot be explained by changed identity fields.
			a.ExpiresAt = time.Now().Unix() + 4
			if _, err := s.db.ExecContext(ctx, `UPDATE principal_credentials SET expires_at=? WHERE id=?`, a.ExpiresAt, a.CredentialID); err != nil {
				t.Fatal(err)
			}
			h := relay.NewHub(s, principal.NewService(s), access.NewService(s), admission.New(admission.Limits{PerDevice: protocol.MaxDeviceRequests}))
			srv := httptest.NewTLSServer(relay.Handler(h, nil))
			defer srv.Close()
			dial := func() *websocket.Conn {
				t.Helper()
				c, _, err := websocket.Dial(ctx, "wss"+srv.URL[5:], &websocket.DialOptions{HTTPClient: srv.Client()})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = c.CloseNow() })
				return c
			}
			write := func(c *websocket.Conn, msg protocol.Message) {
				t.Helper()
				if err := wsjson.Write(ctx, c, msg); err != nil {
					t.Fatal(err)
				}
			}
			read := func(c *websocket.Conn) protocol.Message {
				t.Helper()
				var msg protocol.Message
				if err := wsjson.Read(ctx, c, &msg); err != nil {
					t.Fatal(err)
				}
				return msg
			}
			device := dial()
			write(device, protocol.Message{Type: protocol.TypeHelloDevice, DeviceID: "target", Secret: "secret"})
			if read(device).Type != protocol.TypeHelloOK {
				t.Fatal("device hello failed")
			}
			confirmWireSettings(t, ctx, device, "grant", true)
			client := dial()
			hello := protocol.Message{Type: protocol.TypeHelloClient, TargetID: "target", Grant: "grant", AccountToken: a.Token}
			if operation == "invoke" {
				write(client, hello)
				if read(client).Type != protocol.TypeHelloOK {
					t.Fatal("initial account hello failed")
				}
			}
			held, err := s.db.Conn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer held.Close()
			before := s.db.Stats().WaitCount
			if operation == "hello" {
				write(client, hello)
			} else {
				write(client, protocol.Message{Type: protocol.TypeInvoke, ExecID: "expired", Op: protocol.OpInfo})
			}
			waitCondition(t, func() bool { return s.db.Stats().WaitCount > before })
			if time.Now().Unix() >= a.ExpiresAt {
				t.Fatal("request did not reach storage before deadline")
			}
			time.Sleep(time.Until(time.Unix(a.ExpiresAt, 0)) + 20*time.Millisecond)
			_ = held.Close()
			denied := read(client)
			if denied.Code != protocol.Unauthorized {
				t.Fatalf("expired caller accepted: %+v", denied)
			}
			if operation == "invoke" && denied.ExecID != "expired" {
				t.Fatal("denial lost request identity")
			}
			healthy := dial()
			write(healthy, protocol.Message{Type: protocol.TypeHelloClient, TargetID: "target", Grant: "grant"})
			if read(healthy).Type != protocol.TypeHelloOK {
				t.Fatal("healthy hello failed")
			}
			write(healthy, protocol.Message{Type: protocol.TypeInvoke, ExecID: "healthy", Op: protocol.OpInfo, Payload: []byte(`{"marker":"healthy"}`)})
			routed := read(device)
			if routed.Type != protocol.TypeInvoke || string(routed.Payload) != `{"marker":"healthy"}` {
				t.Fatalf("expired request reached device: %+v", routed)
			}
			write(device, protocol.Message{Type: protocol.TypeExit, ExecID: routed.ExecID, ExitCode: protocol.Int(0), Status: protocol.ExitCompleted})
			if reply := read(healthy); reply.Type != protocol.TypeExit || reply.ExecID != "healthy" {
				t.Fatal("healthy routing did not finish", reply)
			}
		})
	}
}
