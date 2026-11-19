package relay

import (
	"context"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/accesscode"
	"github.com/xallor-ai/xallor-remote/internal/account"
	"github.com/xallor-ai/xallor-remote/internal/admission"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Both callers can access this device, but recipient consent and the resume
// credential belong to Alice. Bob must not reuse them even with the real secret.
func TestSessionResume_shouldRejectOtherPrincipal_onSameDevice(t *testing.T) {
	for _, owned := range []bool{false, true} {
		name := "account-with-grant"
		if owned {
			name = "owned-device"
		}
		t.Run(name, func(t *testing.T) {
			s := openIdentityTestStore(t)
			alice := enrollment(t, s, "resume-alice", "")
			bob := enrollment(t, s, "resume-bob", alice.TenantID)
			h := NewHub(s, s, access.NewService(s), nil, admission.New(admission.Limits{PerDevice: protocol.MaxDeviceRequests}), s.SQLStore)
			device := &recSender{}
			if code := h.RegisterDevice("device", "secret", true, device); code != "" {
				t.Fatal(code)
			}
			grant := "identity-transport-grant"
			h.Handle(device, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeDeviceSettings, RequestID: "settings", GrantHash: protocol.SHA256Hex(grant), Inbound: protocol.Bool(true)})
			if owned {
				controlAccess(t, h, device, alice.Token, access.Request{Action: "claim"})
				controlAccess(t, h, device, alice.Token, access.Request{Action: "share", MembershipID: bob.MembershipID, Operations: []string{protocol.OpInfo}, TTLSeconds: 3600})
			}
			target, err := s.LoadDevice(context.Background(), "device")
			if err != nil {
				t.Fatal(err)
			}
			original := &recSender{}
			if code := h.RegisterClient("device", grant, alice.Token, original); code != "" {
				t.Fatal(code)
			}
			now := time.Now().UTC()
			code := accesscode.Code{ID: "identity-code", DeviceUID: target.UID, OwnershipID: target.OwnershipID, Type: accesscode.OneTime, Generation: 1, IssuedAt: now, Scope: []string{protocol.OpInfo}, RequireApproval: true, State: accesscode.StateActive}
			if err := s.WriteAccessCode(context.Background(), func(tx accesscode.Transaction) error {
				return tx.InsertCode(context.Background(), code, protocol.SHA256Hex("identity-code-secret"))
			}); err != nil {
				t.Fatal(err)
			}
			expires := now.Add(time.Hour).Unix()
			h.Handle(original, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeSessionRequest, RequestID: "identity-request", SessionID: "identity-session", TargetUID: target.UID, CodeID: code.ID, CodeGeneration: 1, SessionScope: []string{protocol.OpInfo}, SessionExpiresAt: &expires, Grant: "identity-code-secret"})
			approval := device.last()
			h.Handle(device, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeSessionApprovalResult, RequestID: approval.RequestID, SessionID: approval.SessionID, RequestSHA256: approval.RequestSHA256, Allow: protocol.Bool(true)})
			active := original.last()
			if active.Status != protocol.SessionActive || active.ResumeCapability == "" {
				t.Fatal("session fixture did not activate")
			}
			if owned {
				// An unused sharing hash does not revoke account-bound consent on
				// an owned device; the original account resume below must still work.
				epoch := h.devices["device"].sessionTarget
				h.Handle(device, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeDeviceSettings, RequestID: "unused-grant-sync", GrantHash: protocol.SHA256Hex("unused-new-grant"), Inbound: protocol.Bool(true)})
				if h.devices["device"].sessionTarget != epoch {
					t.Fatal("unused grant changed owned consent epoch")
				}
			}
			h.Drop(original, protocol.RoleClient, "device")
			other := &recSender{}
			if code := h.RegisterClient("device", grant, bob.Token, other); code != "" {
				t.Fatal(code)
			}
			request := protocol.Message{Type: protocol.TypeSessionResumeRequest, RequestID: "identity-resume", SessionID: active.SessionID, ResumeID: active.ResumeID, ResumeCapability: active.ResumeCapability, TargetUID: target.UID}
			h.Handle(other, protocol.RoleClient, "device", request)
			if result := other.last(); result.Status != protocol.SessionRejectedState || result.Code != protocol.Unauthorized {
				t.Fatalf("another principal resumed session: status=%s code=%s", result.Status, result.Code)
			}
			// An explicit session ID after refused resume must not fall back to
			// the transport grant or independently granted shared-device access.
			h.Handle(other, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeInvoke, ExecID: "refused-session-info", SessionID: active.SessionID, Op: protocol.OpInfo})
			routed := h.requests[requestKey{client: other, id: "refused-session-info"}] != ""
			if result := other.last(); result.Type != protocol.TypeInvokeNack || result.Code != protocol.Unauthorized || routed {
				t.Fatalf("unbound explicit session used transport access: routed=%v type=%s code=%s", routed, result.Type, result.Code)
			}
			// A replacement credential cannot inherit the old credential's consent.
			renewed, err := account.New(s).IssueCredential(context.Background(), account.IssueCredentialRequest{MembershipID: alice.MembershipID, TTL: time.Hour})
			if err != nil {
				t.Fatal(err)
			}
			invalidCallers := map[string]string{"replacement-credential": renewed.Token}
			if !owned {
				invalidCallers["account-downgrade-to-grant"] = ""
			}
			for name, token := range invalidCallers {
				caller := &recSender{}
				if code := h.RegisterClient("device", grant, token, caller); code != "" {
					t.Fatal(code)
				}
				h.Handle(caller, protocol.RoleClient, "device", request)
				if result := caller.last(); result.Status != protocol.SessionRejectedState || result.Code != protocol.Unauthorized {
					t.Fatalf("%s inherited consent", name)
				}
			}
			if err := s.ReadAccessCode(context.Background(), func(r accesscode.Reader) error {
				resume, err := r.Resume(context.Background(), active.ResumeID)
				if err != nil {
					return err
				}
				if resume.State != accesscode.ResumeActive {
					t.Fatal("another principal consumed the capability")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			fresh := &recSender{}
			if code := h.RegisterClient("device", grant, alice.Token, fresh); code != "" {
				t.Fatal(code)
			}
			h.Handle(fresh, protocol.RoleClient, "device", request)
			if fresh.last().Status != protocol.SessionActive {
				t.Fatal("unchanged authorized identity could not resume")
			}
		})
	}
}
