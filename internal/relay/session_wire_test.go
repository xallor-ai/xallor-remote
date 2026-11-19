package relay

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/xallor-ai/xallor-remote/internal/accesscode"
	"github.com/xallor-ai/xallor-remote/internal/admission"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Real TLS sockets exercise the complete non-GUI Relay session path: reserve,
// target approval, final session authorization and a scope-bound invoke.
func TestSessionWire_shouldApproveAndRouteScopedInvoke_overRealRelay(t *testing.T) {
	store := openIdentityTestStore(t)
	h := NewHub(store, nil, nil, nil, admission.New(admission.Limits{PerDevice: protocol.MaxDeviceRequests}), store.SQLStore)
	server := httptest.NewTLSServer(Handler(h, nil))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	dial := func() *websocket.Conn {
		conn, _, err := websocket.Dial(ctx, "wss"+server.URL[5:], &websocket.DialOptions{HTTPClient: server.Client()})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.CloseNow() })
		return conn
	}
	read := func(conn *websocket.Conn) protocol.Message {
		var message protocol.Message
		if err := wsjson.Read(ctx, conn, &message); err != nil {
			t.Fatal(err)
		}
		return message
	}
	write := func(conn *websocket.Conn, message protocol.Message) {
		if err := wsjson.Write(ctx, conn, message); err != nil {
			t.Fatal(err)
		}
	}

	device := dial()
	write(device, protocol.Message{Type: protocol.TypeHelloDevice, DeviceID: "wire-device", Secret: "wire-secret"})
	if hello := read(device); hello.Type != protocol.TypeHelloOK || hello.Role != protocol.RoleDevice {
		t.Fatalf("device hello=%+v", hello)
	}
	grant := "xr_grant_transport"
	write(device, protocol.Message{Type: protocol.TypeDeviceSettings, RequestID: "settings", GrantHash: protocol.SHA256Hex(grant), Inbound: protocol.Bool(true)})
	if ack := read(device); ack.Type != protocol.TypeDeviceSettingsResult || ack.Code != "" {
		t.Fatalf("device settings=%+v", ack)
	}
	client := dial()
	write(client, protocol.Message{Type: protocol.TypeHelloClient, TargetID: "wire-device", Grant: grant})
	if hello := read(client); hello.Type != protocol.TypeHelloOK || hello.Role != protocol.RoleClient {
		t.Fatalf("client hello=%+v", hello)
	}
	target, err := store.LoadDevice(ctx, "wire-device")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	code := accesscode.Code{ID: "wire-code", DeviceUID: target.UID, Type: accesscode.OneTime, Generation: 1, IssuedAt: now, Scope: []string{protocol.OpInfo}, RequireApproval: true, State: accesscode.StateActive}
	if err := store.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		return tx.InsertCode(ctx, code, protocol.SHA256Hex("xr_grant_session"))
	}); err != nil {
		t.Fatal(err)
	}
	expires := now.Add(time.Hour).Unix()
	write(client, protocol.Message{Type: protocol.TypeSessionRequest, RequestID: "wire-request", SessionID: "wire-session", TargetID: "wire-device", TargetUID: target.UID, CodeID: code.ID, CodeGeneration: 1, SessionScope: []string{protocol.OpInfo}, SessionExpiresAt: &expires, Grant: "xr_grant_session"})
	approval := read(device)
	if approval.Type != protocol.TypeSessionApprovalRequest || approval.RequestSHA256 == "" || approval.TargetUID != target.UID {
		t.Fatalf("approval=%+v", approval)
	}
	if pending := read(client); pending.Type != protocol.TypeSessionResult || pending.Status != protocol.SessionPending {
		t.Fatalf("pending=%+v", pending)
	}
	write(device, protocol.Message{Type: protocol.TypeSessionApprovalResult, RequestID: approval.RequestID, SessionID: approval.SessionID, RequestSHA256: approval.RequestSHA256, Allow: protocol.Bool(true)})
	if active := read(client); active.Type != protocol.TypeSessionResult || active.Status != protocol.SessionActive {
		t.Fatalf("active=%+v", active)
	}
	if deviceActive := read(device); deviceActive.Type != protocol.TypeSessionResult || deviceActive.Status != protocol.SessionActive {
		t.Fatalf("device active=%+v", deviceActive)
	}
	write(client, protocol.Message{Type: protocol.TypeInvoke, ExecID: "wire-exec", SessionID: "wire-session", Op: protocol.OpInfo})
	routed := read(device)
	if routed.Type != protocol.TypeInvoke || routed.Op != protocol.OpInfo || routed.ExecID == "wire-exec" {
		t.Fatalf("routed=%+v", routed)
	}
	write(device, protocol.Message{Type: protocol.TypeExit, ExecID: routed.ExecID, ExitCode: protocol.Int(0), Status: protocol.ExitCompleted})
	if result := read(client); result.Type != protocol.TypeExit || result.ExecID != "wire-exec" {
		t.Fatalf("result=%+v", result)
	}
}
