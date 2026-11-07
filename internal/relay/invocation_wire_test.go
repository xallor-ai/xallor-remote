package relay

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/admission"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Real TLS connections reproduce the old disconnect/reuse attack with two
// authenticated principals. Ordering on the device socket proves Drop finished
// before the second invocation; no timing sleeps or speculative negative reads.
func TestRoutingWire_should_dropLatePrivateResult_whenAnotherPrincipalReusesID(t *testing.T) {
	s := openIdentityTestStore(t)
	a, b := enrollment(t, s, "alice", ""), enrollment(t, s, "bob", "")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	deviceRecord, err := s.RegisterDevice(ctx, "device", protocol.SHA256Hex("secret"), true)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := access.NewService(s).Manage(ctx, "device", deviceRecord, a.Identity, access.Request{Action: "claim"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = access.NewService(s).Manage(ctx, "device", claimed.Device, a.Identity, access.Request{Action: "share", MembershipID: b.MembershipID, Operations: []string{protocol.OpHandoffGet}, TTLSeconds: 3600}); err != nil {
		t.Fatal(err)
	}
	h := NewHub(s, s, access.NewService(s), nil, admission.New(admission.Limits{PerDevice: protocol.MaxDeviceRequests}))
	srv := httptest.NewTLSServer(Handler(h, nil))
	defer srv.Close()
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
			t.Fatal(m)
		}
		return c
	}
	d := dial(protocol.Message{Type: protocol.TypeHelloDevice, DeviceID: "device", Secret: "secret"})
	confirmWireSettings(t, ctx, d, "grant", true)
	first := dial(protocol.Message{Type: protocol.TypeHelloClient, TargetID: "device", AccountToken: a.Token})
	write(first, protocol.Message{Type: protocol.TypeInvoke, Op: protocol.OpHandoffGet, ExecID: "same"})
	old := read(d)
	if old.Scope == nil || old.Scope.PrincipalID != a.PrincipalID || old.ExecID == "same" {
		t.Fatal("first invocation lacked internal identity")
	}
	_ = first.CloseNow()
	if m := read(d); m.Op != protocol.OpCancel || m.ExecID != old.ExecID {
		t.Fatal("disconnect cancellation not bound to first invocation", m)
	}
	second := dial(protocol.Message{Type: protocol.TypeHelloClient, TargetID: "device", AccountToken: b.Token})
	write(second, protocol.Message{Type: protocol.TypeInvoke, Op: protocol.OpHandoffGet, ExecID: "same"})
	fresh := read(d)
	if fresh.Scope == nil || fresh.Scope.PrincipalID != b.PrincipalID || fresh.ExecID == old.ExecID {
		t.Fatal("replacement reused internal identity")
	}
	write(d, protocol.Message{Type: protocol.TypeStdout, ExecID: old.ExecID, Data: "alice-private"})
	write(d, protocol.Message{Type: protocol.TypeExit, ExecID: old.ExecID, ExitCode: protocol.Int(0)})
	write(d, protocol.Message{Type: protocol.TypeStdout, ExecID: fresh.ExecID, Data: "bob-result"})
	write(d, protocol.Message{Type: protocol.TypeExit, ExecID: fresh.ExecID, ExitCode: protocol.Int(0)})
	if m := read(second); m.Type != protocol.TypeStdout || m.ExecID != "same" || m.Data != "bob-result" {
		t.Fatal("another principal's output was delivered", m)
	}
	if m := read(second); m.Type != protocol.TypeExit || m.ExecID != "same" {
		t.Fatal("replacement terminal missing", m)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.requests) != 0 || len(h.inflight) != 0 {
		t.Fatal("wire terminal leaked mappings")
	}
}
