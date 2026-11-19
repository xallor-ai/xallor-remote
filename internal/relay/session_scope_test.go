package relay

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/accesscode"
	"github.com/xallor-ai/xallor-remote/internal/admission"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

func sessionScopeFixture(t *testing.T) (*relaySQLFixture, *Hub, *recSender, *recSender, protocol.Message) {
	t.Helper()
	s := openIdentityTestStore(t)
	h := NewHub(s, nil, nil, nil, admission.New(admission.Limits{PerDevice: protocol.MaxDeviceRequests}), s.SQLStore)
	device, client := &recSender{}, &recSender{}
	if code := h.RegisterDevice("device", "secret", true, device); code != "" {
		t.Fatal(code)
	}
	h.Handle(device, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeDeviceSettings, RequestID: "settings", GrantHash: protocol.SHA256Hex("transport"), Inbound: protocol.Bool(true)})
	if code := h.RegisterClient("device", "transport", "", client); code != "" {
		t.Fatal(code)
	}
	ctx := context.Background()
	target, err := s.LoadDevice(ctx, "device")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	code := accesscode.Code{ID: "scoped-code", DeviceUID: target.UID, Type: accesscode.OneTime, Generation: 2, IssuedAt: now, Scope: []string{protocol.OpInfo, protocol.OpExec}, RequireApproval: true, State: accesscode.StateActive}
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		return tx.InsertCode(ctx, code, protocol.SHA256Hex("code-secret"))
	}); err != nil {
		t.Fatal(err)
	}
	expires := now.Add(time.Hour).Unix()
	return s, h, device, client, protocol.Message{Type: protocol.TypeSessionRequest, RequestID: "scope-request", SessionID: "scope-session", TargetUID: target.UID, CodeID: code.ID, CodeGeneration: code.Generation, SessionScope: []string{protocol.OpInfo}, SessionExpiresAt: &expires, Grant: "code-secret"}
}

// Recipient approves info only on a code also allowing exec. Neither the stored
// session nor a resumed connection may inherit the code's broader permissions.
func TestSessionScope_shouldPersistOnlyApprovedSubset_throughResume(t *testing.T) {
	s, h, device, client, request := sessionScopeFixture(t)
	h.Handle(client, protocol.RoleClient, "device", request)
	approval := device.last()
	if approval.Type != protocol.TypeSessionApprovalRequest || len(approval.SessionScope) != 1 || approval.SessionScope[0] != protocol.OpInfo {
		t.Fatal("wrong consent request")
	}
	h.Handle(device, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeSessionApprovalResult, RequestID: approval.RequestID, SessionID: approval.SessionID, RequestSHA256: approval.RequestSHA256, Allow: protocol.Bool(true)})
	active := client.last()
	if active.Status != protocol.SessionActive {
		t.Fatal("approval failed")
	}
	if err := s.ReadAccessCode(context.Background(), func(r accesscode.Reader) error {
		session, err := r.Session(context.Background(), request.SessionID)
		if err != nil {
			return err
		}
		if len(session.Scope) != 1 || session.Scope[0] != protocol.OpInfo {
			return fmt.Errorf("stored scope exceeds consent: %v", session.Scope)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if h.verifySessionAccess(client, request.SessionID, protocol.OpExec) != protocol.Unauthorized {
		t.Fatal("exec inherited without consent")
	}
	h.Drop(client, protocol.RoleClient, "device")
	fresh := &recSender{}
	if code := h.RegisterClient("device", "transport", "", fresh); code != "" {
		t.Fatal(code)
	}
	h.Handle(fresh, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeSessionResumeRequest, RequestID: "resume", SessionID: request.SessionID, ResumeID: active.ResumeID, ResumeCapability: active.ResumeCapability, TargetUID: request.TargetUID})
	if fresh.last().Status != protocol.SessionActive || h.verifySessionAccess(fresh, request.SessionID, protocol.OpInfo) != "" {
		t.Fatal("approved subset cannot resume")
	}
	if h.verifySessionAccess(fresh, request.SessionID, protocol.OpExec) != protocol.Unauthorized {
		t.Fatal("resume broadened approved scope")
	}
}

// Bad generation and operations outside the code must fail before creating a
// reservation or asking the recipient, leaving the one-time code unconsumed.
func TestSessionScope_shouldRejectGenerationOrScopeMismatch_beforeReservation(t *testing.T) {
	for _, name := range []string{"generation", "scope"} {
		t.Run(name, func(t *testing.T) {
			s, h, device, client, request := sessionScopeFixture(t)
			if name == "generation" {
				request.CodeGeneration--
			} else {
				request.SessionScope = []string{protocol.OpWrite}
			}
			h.Handle(client, protocol.RoleClient, "device", request)
			result := client.last()
			if result.Status != protocol.SessionRejectedState || result.Code != protocol.Unauthorized {
				t.Fatalf("invalid %s accepted: status=%s code=%s", name, result.Status, result.Code)
			}
			if device.last().Type == protocol.TypeSessionApprovalRequest {
				t.Fatal("invalid request reached recipient")
			}
			if err := s.ReadAccessCode(context.Background(), func(r accesscode.Reader) error {
				if _, err := r.Reservation(context.Background(), request.RequestID); err == nil {
					return fmt.Errorf("invalid request reserved code")
				}
				code, err := r.Code(context.Background(), request.CodeID)
				if err != nil {
					return err
				}
				if code.State != accesscode.StateActive {
					return fmt.Errorf("invalid request consumed code")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
