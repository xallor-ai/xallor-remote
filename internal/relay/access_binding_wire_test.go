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

// Real WSS and SQL connections retain an old client across rotate/recreate.
// Reusing the original code must require a fresh handshake before any invocation.
func TestAccessWire_should_rejectOldBinding_whenCredentialReused(t *testing.T) {
	for _, scenario := range []string{"rotate", "recreate"} {
		t.Run(scenario, func(t *testing.T) {
			s := openIdentityTestStore(t)
			h := NewHub(s)
			srv := httptest.NewTLSServer(Handler(h, nil))
			defer srv.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			write := func(c *websocket.Conn, m protocol.Message) {
				t.Helper()
				if err := wsjson.Write(ctx, c, m); err != nil {
					t.Fatal(err)
				}
			}
			read := func(c *websocket.Conn) protocol.Message {
				t.Helper()
				var m protocol.Message
				if err := wsjson.Read(ctx, c, &m); err != nil {
					t.Fatal(err)
				}
				return m
			}
			dial := func(hello protocol.Message) *websocket.Conn {
				t.Helper()
				c, _, err := websocket.Dial(ctx, "wss"+srv.URL[5:], &websocket.DialOptions{HTTPClient: srv.Client()})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = c.CloseNow() })
				write(c, hello)
				if m := read(c); m.Type != protocol.TypeHelloOK {
					t.Fatalf("hello: %+v", m)
				}
				return c
			}
			hello := protocol.Message{Type: protocol.TypeHelloDevice, DeviceID: "123456789", Secret: "secret", Inbound: protocol.Bool(true)}
			device := dial(hello)
			rotate := func(code string) {
				write(device, protocol.Message{Type: protocol.TypeGrantRotate, NewGrantHash: protocol.SHA256Hex(code)})
				waitCondition(t, func() bool {
					h.mu.Lock()
					defer h.mu.Unlock()
					return h.devices[hello.DeviceID].record.GrantHash == protocol.SHA256Hex(code)
				})
			}
			rotate("shared")
			clientHello := protocol.Message{Type: protocol.TypeHelloClient, TargetID: hello.DeviceID, Grant: "shared"}
			stale := dial(clientHello)
			before, err := s.LoadDevice(ctx, hello.DeviceID)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "recreate" {
				write(device, protocol.Message{Type: protocol.TypeRevoke})
				waitCondition(t, func() bool { return !h.DeviceOnline(hello.DeviceID) })
				device = dial(hello)
			} else {
				rotate("temporary")
			}
			rotate("shared")
			after, err := s.LoadDevice(ctx, hello.DeviceID)
			if err != nil || before.Binding().Matches(after.Binding()) {
				t.Fatalf("binding reused: %+v %v", after, err)
			}
			write(stale, protocol.Message{Type: protocol.TypeInvoke, Op: protocol.OpInfo, ExecID: "stale"})
			if m := read(stale); m.Type != protocol.TypeInvokeNack || m.Code != protocol.Unauthorized {
				t.Fatalf("stale socket accepted: %+v", m)
			}
			fresh := dial(clientHello)
			write(fresh, protocol.Message{Type: protocol.TypeInvoke, Op: protocol.OpInfo, ExecID: "fresh"})
			if m := read(device); m.ExecID != "fresh" {
				t.Fatalf("wrong invocation reached device: %+v", m)
			}
			write(device, protocol.Message{Type: protocol.TypeExit, ExecID: "fresh", ExitCode: protocol.Int(0), Status: protocol.ExitCompleted})
			if m := read(fresh); m.Type != protocol.TypeExit || m.ExecID != "fresh" {
				t.Fatalf("fresh result missing: %+v", m)
			}
		})
	}
}
