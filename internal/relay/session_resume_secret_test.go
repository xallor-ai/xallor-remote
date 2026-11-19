package relay

import (
	"context"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/accesscode"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Knowing public IDs and sharing the transport binding must not authorize
// resume with a wrong secret or with the stored hash used as a bearer.
func TestSessionResume_shouldRejectWrongSecret_withoutConsumingOrBinding(t *testing.T) {
	for _, kind := range []string{"wrong", "stored-hash", "empty"} {
		t.Run(kind, func(t *testing.T) {
			store, h, _, original := approvedSessionFixture(t)
			active := original.last()
			h.Drop(original, protocol.RoleClient, "device")
			caller := &recSender{}
			if code := h.RegisterClient("device", "xr_grant_transport", "", caller); code != "" {
				t.Fatal(code)
			}
			h.mu.Lock()
			uid := h.devices["device"].record.UID
			h.mu.Unlock()
			secret := "incorrect-capability"
			if kind == "stored-hash" {
				secret = protocol.SHA256Hex(active.ResumeCapability)
			}
			if kind == "empty" {
				secret = ""
			}
			request := protocol.Message{Type: protocol.TypeSessionResumeRequest, RequestID: "wrong-secret", SessionID: active.SessionID, ResumeID: active.ResumeID, ResumeCapability: secret, TargetUID: uid}
			h.Handle(caller, protocol.RoleClient, "device", request)
			result := caller.last()
			if result.Status != protocol.SessionRejectedState || result.Code != protocol.Unauthorized {
				t.Fatalf("wrong credential accepted: status=%s code=%s", result.Status, result.Code)
			}
			if result.ResumeID != "" || result.ResumeCapability != "" {
				t.Fatal("rejection disclosed a replacement credential")
			}
			h.mu.Lock()
			bound := h.clients[caller].session != nil
			h.mu.Unlock()
			if bound {
				t.Fatal("rejection bound a session")
			}
			if err := store.ReadAccessCode(context.Background(), func(r accesscode.Reader) error {
				resume, err := r.Resume(context.Background(), active.ResumeID)
				if err != nil {
					return err
				}
				if resume.State != accesscode.ResumeActive {
					t.Fatal("wrong secret consumed valid capability")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			request.RequestID, request.ResumeCapability = "correct-secret", active.ResumeCapability
			h.Handle(caller, protocol.RoleClient, "device", request)
			if caller.last().Status != protocol.SessionActive {
				t.Fatal("valid credential failed after rejected attempt")
			}
		})
	}
}
