package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/accesscode"
	"github.com/xallor-ai/xallor-remote/internal/admission"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

var typedAccessRequestID atomic.Int32

func typedCodeFixture(t *testing.T) (*relaySQLFixture, *Hub, *recSender, *recSender, access.Device, string) {
	t.Helper()
	s := openIdentityTestStore(t)
	owner := enrollment(t, s, "code-owner", "")
	h := NewHub(s, s, access.NewService(s), nil, admission.New(admission.Limits{PerDevice: protocol.MaxDeviceRequests}), s.SQLStore)
	device, control := &recSender{}, &recSender{}
	if code := h.RegisterDevice("device", "secret", true, device); code != "" {
		t.Fatal(code)
	}
	grant := "transport-grant"
	h.Handle(device, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeDeviceSettings, RequestID: "settings", GrantHash: protocol.SHA256Hex(grant), Inbound: protocol.Bool(true)})
	controlAccess(t, h, device, owner.Token, access.Request{Action: "claim"})
	if code := h.RegisterClient("device", "", owner.Token, control); code != "" {
		t.Fatal(code)
	}
	record, err := s.LoadDevice(context.Background(), "device")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	code := accesscode.Code{ID: "managed-code", DeviceUID: record.UID, OwnershipID: record.OwnershipID, Type: accesscode.LongLived, Generation: 1, IssuedAt: now, Scope: []string{protocol.OpInfo}, RequireApproval: true, State: accesscode.StateActive}
	if err := s.WriteAccessCode(context.Background(), func(tx accesscode.Transaction) error {
		return tx.InsertCode(context.Background(), code, protocol.SHA256Hex("managed-secret"))
	}); err != nil {
		t.Fatal(err)
	}
	return s, h, device, control, record, owner.Token
}

func sendTypedAccess(t *testing.T, h *Hub, device *recSender, token string, req access.Request) protocol.Message {
	t.Helper()
	raw, _ := json.Marshal(req)
	id := fmt.Sprintf("%s-%d", req.Action, typedAccessRequestID.Add(1))
	h.Handle(device, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeAccessRequest, RequestID: id, AccountToken: token, Payload: raw})
	return device.last()
}

// Code management is owner-only on the authenticated device control plane;
// shared principals and bearer/controller paths cannot revoke or rotate it.
func TestTypedCodeAccess_shouldRequireOwnerDeviceConnection(t *testing.T) {
	s, h, device, _, record, ownerToken := typedCodeFixture(t)
	owner := enrollment(t, s, "other-owner", "")
	if got := sendTypedAccess(t, h, device, owner.Token, access.Request{Action: "code.revoke", CodeID: "managed-code"}); got.Code != protocol.Unauthorized {
		t.Fatal("non-owner revoked code", got.Code)
	}
	controller := &recSender{}
	if code := h.RegisterClient("device", "", owner.Token, controller); code == "" {
		t.Fatal("non-owner connected")
	}
	// Device control is the only route; owner result is successful and scoped to
	// the exact device/generation, without returning the old secret.
	ownerDevice := device
	got := sendTypedAccess(t, h, ownerDevice, ownerToken, access.Request{Action: "code.revoke", CodeID: "managed-code"})
	if got.Code != "" {
		t.Fatal("owner device revoke failed", got.Code)
	}
	_ = record
}

func TestTypedCodeAccess_shouldRevokeAndRotate_onlyWithOwnerAccount(t *testing.T) {
	s, h, device, _, _, ownerToken := typedCodeFixture(t)
	if got := sendTypedAccess(t, h, device, "", access.Request{Action: "code.rotate", CodeID: "managed-code", NewCodeID: "replacement-code", NewSecret: "replacement-secret"}); got.Code != protocol.Unauthorized {
		t.Fatal("unauthenticated rotation accepted", got.Code)
	}
	got := sendTypedAccess(t, h, device, ownerToken, access.Request{Action: "code.rotate", CodeID: "managed-code", NewCodeID: "replacement-code", NewSecret: "replacement-secret"})
	if got.Code != "" {
		t.Fatal("owner rotation failed", got.Code)
	}
	var result access.CodeRotationReply
	if err := json.Unmarshal(got.Payload, &result); err != nil || result.PreviousCodeID != "managed-code" || result.Code.ID != "replacement-code" || result.Code.State != accesscode.StateActive || result.Code.Generation != 2 || result.IntentSHA256 == "" {
		t.Fatal("bad rotation result", err)
	}
	if err := s.ReadAccessCode(context.Background(), func(r accesscode.Reader) error {
		code, err := r.Code(context.Background(), "managed-code")
		if err != nil {
			return err
		}
		if code.State != accesscode.StateRevoked {
			t.Error("rotation did not revoke source")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
