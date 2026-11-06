package relay

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Exhaust the real SQL pool during a WSS hello. Existing traffic must still finish;
// closing that unacknowledged socket must cancel admission without creating a ghost device.
func TestIdentityWire_should_routeAndCancelDisconnectedHello_whenSQLPoolBlocked(t *testing.T) {
	s := openIdentityTestStore(t)
	if _, err := s.RegisterDevice(context.Background(), "healthy", protocol.SHA256Hex("secret"), true); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateDevice(context.Background(), "healthy", DeviceRecord{SecretHash: protocol.SHA256Hex("secret"), GrantHash: protocol.SHA256Hex("grant"), Inbound: true}); err != nil {
		t.Fatal(err)
	}
	h := NewHub(s)
	srv := httptest.NewTLSServer(Handler(h, nil))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	dial := func(hello protocol.Message, acknowledge bool) *websocket.Conn {
		t.Helper()
		c, _, err := websocket.Dial(ctx, "wss"+srv.URL[5:], &websocket.DialOptions{HTTPClient: srv.Client()})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.CloseNow() })
		if err := wsjson.Write(ctx, c, hello); err != nil {
			t.Fatal(err)
		}
		if acknowledge {
			var reply protocol.Message
			if err := wsjson.Read(ctx, c, &reply); err != nil || reply.Type != protocol.TypeHelloOK {
				t.Fatalf("hello: %+v %v", reply, err)
			}
		}
		return c
	}
	device := dial(protocol.Message{Type: protocol.TypeHelloDevice, DeviceID: "healthy", Secret: "secret"}, true)
	client := dial(protocol.Message{Type: protocol.TypeHelloClient, TargetID: "healthy", Grant: "grant"}, true)
	held, err := s.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	before := s.db.Stats().WaitCount
	pending := dial(protocol.Message{Type: protocol.TypeHelloDevice, DeviceID: "pending", Secret: "other"}, false)
	waitCondition(t, func() bool { return s.db.Stats().WaitCount > before })
	fast, stop := context.WithTimeout(ctx, 2*time.Second)
	defer stop()
	if err := wsjson.Write(fast, client, protocol.Message{Type: protocol.TypeInvoke, ExecID: "still-live", Op: protocol.OpInfo}); err != nil {
		t.Fatal(err)
	}
	var message protocol.Message
	if err := wsjson.Read(fast, device, &message); err != nil || message.ExecID != "still-live" {
		t.Fatalf("routing blocked: %+v %v", message, err)
	}
	if err := wsjson.Write(fast, device, protocol.Message{Type: protocol.TypeExit, ExecID: "still-live", ExitCode: protocol.Int(0), Status: protocol.ExitCompleted}); err != nil {
		t.Fatal(err)
	}
	if err := wsjson.Read(fast, client, &message); err != nil || message.Type != protocol.TypeExit {
		t.Fatalf("result blocked: %+v %v", message, err)
	}
	_ = pending.CloseNow()
	waitCondition(t, func() bool {
		h.identities.mu.Lock()
		defer h.identities.mu.Unlock()
		return h.identities.entries["pending"] == nil
	})
	if h.DeviceOnline("pending") {
		t.Fatal("disconnected hello became routable")
	}
	if err := held.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadDevice(ctx, "pending"); !errors.Is(err, ErrDeviceNotFound) {
		t.Fatalf("disconnected hello persisted identity: %v", err)
	}
}
