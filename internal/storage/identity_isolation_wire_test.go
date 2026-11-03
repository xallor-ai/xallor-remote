package storage

import (
	"context"
	"errors"
	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/relay"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/xallor-ai/xallor-remote/internal/admission"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Exhaust the real SQL pool during a WSS hello. Existing traffic must still finish;
// closing that unacknowledged socket must cancel admission without creating a ghost device.
func TestIdentityWire_should_routeAndCancelDisconnectedHello_whenSQLPoolBlocked(t *testing.T) {
	s := openIdentityTestStore(t)
	if _, err := s.RegisterDevice(context.Background(), "healthy", protocol.SHA256Hex("secret"), true); err != nil {
		t.Fatal(err)
	}
	setTestAccess(t, s, "healthy", protocol.SHA256Hex("grant"), true)
	h := relay.NewHub(s, nil, nil, admission.New(admission.Limits{PerDevice: protocol.MaxDeviceRequests}))
	srv := httptest.NewTLSServer(relay.Handler(h, nil))
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
	confirmWireSettings(t, ctx, device, "grant", true)
	client := dial(protocol.Message{Type: protocol.TypeHelloClient, TargetID: "healthy", Grant: "grant"}, true)
	held, err := s.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	before := s.db.Stats().WaitCount
	waited := s.db.Stats().WaitDuration
	pending := dial(protocol.Message{Type: protocol.TypeHelloDevice, DeviceID: "pending", Secret: "other"}, false)
	waitCondition(t, func() bool { return s.db.Stats().WaitCount > before })
	fast, stop := context.WithTimeout(ctx, 2*time.Second)
	defer stop()
	if err := wsjson.Write(fast, client, protocol.Message{Type: protocol.TypeInvoke, ExecID: "still-live", Op: protocol.OpInfo}); err != nil {
		t.Fatal(err)
	}
	var message protocol.Message
	if err := wsjson.Read(fast, device, &message); err != nil || message.Type != protocol.TypeInvoke || message.Op != protocol.OpInfo {
		t.Fatalf("routing blocked: %+v %v", message, err)
	}
	if err := wsjson.Write(fast, device, protocol.Message{Type: protocol.TypeExit, ExecID: message.ExecID, ExitCode: protocol.Int(0), Status: protocol.ExitCompleted}); err != nil {
		t.Fatal(err)
	}
	if err := wsjson.Read(fast, client, &message); err != nil || message.Type != protocol.TypeExit {
		t.Fatalf("result blocked: %+v %v", message, err)
	}
	_ = pending.CloseNow()
	waitCondition(t, func() bool { return s.db.Stats().WaitDuration > waited })
	if h.DeviceOnline("pending") {
		t.Fatal("disconnected hello became routable")
	}
	if err := held.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadDevice(ctx, "pending"); !errors.Is(err, access.ErrDeviceNotFound) {
		t.Fatalf("disconnected hello persisted identity: %v", err)
	}
}
