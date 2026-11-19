package relay

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/coder/websocket/wsjson"
	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/accesscode"
	"github.com/xallor-ai/xallor-remote/internal/admission"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Owner code management must traverse a real Relay WebSocket/device control
// channel; shared/controller connections and bad principals fail closed.
func TestCodeManagementWire_shouldRevokeAndRotate_overAuthenticatedDeviceSocket(t *testing.T) {
	s := openIdentityTestStore(t)
	owner := enrollment(t, s, "wire-owner", "")
	other := enrollment(t, s, "wire-other", "")
	h := NewHub(s, s, access.NewService(s), nil, admission.New(admission.Limits{PerDevice: protocol.MaxDeviceRequests}), s.SQLStore)
	dctx, device, hello := identityWire(t, h, protocol.Message{Type: protocol.TypeHelloDevice, DeviceID: "device", Secret: "secret"})
	if hello.Type != protocol.TypeHelloOK {
		t.Fatal(hello.Code)
	}
	confirmWireSettings(t, dctx, device, "wire-grant", true)
	claimRaw, _ := json.Marshal(access.Request{Action: "claim"})
	if err := wsjson.Write(dctx, device, protocol.Message{Type: protocol.TypeAccessRequest, RequestID: "claim", AccountToken: owner.Token, Payload: claimRaw}); err != nil {
		t.Fatal(err)
	}
	var claimReply protocol.Message
	if err := wsjson.Read(dctx, device, &claimReply); err != nil || claimReply.Code != "" {
		t.Fatal("wire claim failed", claimReply.Code, err)
	}
	record, err := s.LoadDevice(context.Background(), "device")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	code := accesscode.Code{ID: "wire-code", DeviceUID: record.UID, OwnershipID: record.OwnershipID, Type: accesscode.LongLived, Generation: 1, IssuedAt: now, Scope: []string{protocol.OpInfo}, RequireApproval: true, State: accesscode.StateActive}
	if err := s.WriteAccessCode(context.Background(), func(tx accesscode.Transaction) error {
		return tx.InsertCode(context.Background(), code, protocol.SHA256Hex("wire-secret"))
	}); err != nil {
		t.Fatal(err)
	}
	send := func(id, token string, req access.Request) protocol.Message {
		raw, _ := json.Marshal(req)
		if err := wsjson.Write(dctx, device, protocol.Message{Type: protocol.TypeAccessRequest, RequestID: id, AccountToken: token, Payload: raw}); err != nil {
			t.Fatal(err)
		}
		var reply protocol.Message
		if err := wsjson.Read(dctx, device, &reply); err != nil {
			t.Fatal(err)
		}
		return reply
	}
	if got := send("bad", other.Token, access.Request{Action: "code.revoke", CodeID: code.ID}); got.Code != protocol.Unauthorized {
		t.Fatal("other principal managed code", got.Code)
	}
	if got := send("revoke", owner.Token, access.Request{Action: "code.revoke", CodeID: code.ID}); got.Code != "" {
		t.Fatal("owner revoke failed", got.Code)
	}
	if got := send("rotate", owner.Token, access.Request{Action: "code.rotate", CodeID: code.ID, NewCodeID: "wire-new", NewSecret: "wire-new-secret"}); got.Code != protocol.Unauthorized {
		t.Fatal("revoked code rotated", got.Code)
	}
	if _, err := s.LoadDevice(context.Background(), "device"); err != nil {
		t.Fatal(err)
	}
}
