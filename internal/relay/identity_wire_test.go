package relay

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

func identityWire(t *testing.T, hub *Hub, hello protocol.Message) (context.Context, *websocket.Conn, protocol.Message) {
	t.Helper()
	srv := httptest.NewTLSServer(Handler(hub, nil))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	c, _, err := websocket.Dial(ctx, "wss"+srv.URL[5:], &websocket.DialOptions{HTTPClient: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.CloseNow() })
	if err := wsjson.Write(ctx, c, hello); err != nil {
		t.Fatal(err)
	}
	var reply protocol.Message
	if err := wsjson.Read(ctx, c, &reply); err != nil {
		t.Fatal(err)
	}
	return ctx, c, reply
}

// The actual TLS WebSocket endpoint must reject both reads and inserts that fail in SQLite.
func TestIdentityWire_should_rejectHello_whenPersistenceFails(t *testing.T) {
	for _, fault := range []string{"lookup", "insert"} {
		t.Run(fault, func(t *testing.T) {
			s := openIdentityTestStore(t)
			statement := `DROP TABLE devices`
			if fault == "insert" {
				statement = `CREATE TRIGGER reject_insert BEFORE INSERT ON devices BEGIN SELECT RAISE(ABORT, 'fixture insert failure'); END;`
			}
			if _, err := s.db.Exec(statement); err != nil {
				t.Fatal(err)
			}
			hub := NewHub(s, nil, nil)
			ctx, c, reply := identityWire(t, hub, protocol.Message{Type: protocol.TypeHelloDevice, DeviceID: "device", Secret: "fixture-secret"})
			if reply.Type != protocol.TypeError || reply.Code != protocol.RelayError {
				t.Fatalf("hello: %+v", reply)
			}
			if hub.DeviceOnline("device") {
				t.Fatal("failed hello registered device")
			}
			var next protocol.Message
			if err := wsjson.Read(ctx, c, &next); err == nil {
				t.Fatalf("failed hello remained connected: %+v", next)
			}
		})
	}
}

// A failed control write must report a connection error over WSS and preserve persisted access state.
func TestIdentityWire_should_closeDevice_whenAuthorizationCannotPersist(t *testing.T) {
	s := openIdentityTestStore(t)
	hub := NewHub(s, nil, nil)
	ctx, c, reply := identityWire(t, hub, protocol.Message{Type: protocol.TypeHelloDevice, DeviceID: "device", Secret: "fixture-secret", Inbound: protocol.Bool(true)})
	if reply.Type != protocol.TypeHelloOK {
		t.Fatalf("hello: %+v", reply)
	}
	if _, err := s.db.Exec(`CREATE TRIGGER reject_update BEFORE UPDATE ON devices BEGIN SELECT RAISE(ABORT, 'fixture update failure'); END;`); err != nil {
		t.Fatal(err)
	}
	if err := wsjson.Write(ctx, c, protocol.Message{Type: protocol.TypeDeviceSettings, RequestID: "settings", Inbound: protocol.Bool(true), GrantHash: protocol.SHA256Hex("new")}); err != nil {
		t.Fatal(err)
	}
	if err := wsjson.Read(ctx, c, &reply); err != nil {
		t.Fatal(err)
	}
	if reply.Type != protocol.TypeError || reply.Code != protocol.RelayError {
		t.Fatalf("write: %+v", reply)
	}
	if err := wsjson.Read(ctx, c, &reply); err == nil {
		t.Fatal("failed mutation left device connection open")
	}
	stored, err := s.LoadDevice(context.Background(), "device")
	if err != nil || stored.GrantHash != "" || stored.Inbound {
		t.Fatalf("persisted state changed: %+v %v", stored, err)
	}
}
