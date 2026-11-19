package relay

import (
	"context"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/accesscode"
	"github.com/xallor-ai/xallor-remote/internal/admission"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// A resumable session can outlive its client transport, but recipient consent
// must not survive target loss, replacement, or a new Relay routing incarnation.
// Real SQLite rows are inspected; no connected client remains to enumerate.
func TestSessionTarget_shouldInvalidateRetainedConsent_whenTargetLifecycleChanges(t *testing.T) {
	for _, change := range []string{"disconnect", "replacement", "relay-restart"} {
		t.Run(change, func(t *testing.T) {
			s, h, target, client := approvedSessionFixture(t)
			active := client.last()
			uid := h.devices["device"].record.UID
			h.Drop(client, protocol.RoleClient, "device")
			if len(h.clients) != 0 {
				t.Fatal("fixture kept a connected controller")
			}
			if change == "disconnect" {
				h.Drop(target, protocol.RoleDevice, "device")
				assertRetiredTargetConsent(t, s, active)
			}
			if change == "relay-restart" {
				h = NewHub(s, nil, nil, nil, admission.New(admission.Limits{PerDevice: protocol.MaxDeviceRequests}), s.SQLStore)
			}
			replacement := &recSender{}
			if code := h.RegisterDevice("device", "secret", true, replacement); code != "" {
				t.Fatal(code)
			}
			h.Handle(replacement, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeDeviceSettings, RequestID: "new-settings", GrantHash: protocol.SHA256Hex("xr_grant_transport"), Inbound: protocol.Bool(true)})
			if err := s.ReadAccessCode(context.Background(), func(r accesscode.Reader) error {
				session, err := r.Session(context.Background(), active.SessionID)
				if err != nil {
					return err
				}
				resume, err := r.Resume(context.Background(), active.ResumeID)
				if err != nil {
					return err
				}
				if session.State != accesscode.SessionRevoked || resume.State != accesscode.ResumeRevoked {
					t.Errorf("retained consent survived target %s: session=%s resume=%s", change, session.State, resume.State)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			fresh := &recSender{}
			if code := h.RegisterClient("device", "xr_grant_transport", "", fresh); code != "" {
				t.Fatal(code)
			}
			h.Handle(fresh, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeSessionResumeRequest, RequestID: "resume-old-target", SessionID: active.SessionID, ResumeID: active.ResumeID, ResumeCapability: active.ResumeCapability, TargetUID: uid})
			if got := fresh.last(); got.Status == protocol.SessionActive || got.ResumeCapability != "" {
				t.Errorf("old recipient consent resumed after target %s", change)
			}
		})
	}
}

func assertRetiredTargetConsent(t *testing.T, s *relaySQLFixture, active protocol.Message) {
	t.Helper()
	if err := s.ReadAccessCode(context.Background(), func(r accesscode.Reader) error {
		ss, err := r.Session(context.Background(), active.SessionID)
		if err != nil {
			return err
		}
		proof, err := r.Resume(context.Background(), active.ResumeID)
		if err != nil {
			return err
		}
		code, err := r.Code(context.Background(), ss.CodeID)
		if err != nil {
			return err
		}
		if ss.State != accesscode.SessionRevoked || proof.State != accesscode.ResumeRevoked || code.State != accesscode.StateConsumed {
			t.Errorf("consent retirement/refund mismatch: session=%s proof=%s code=%s", ss.State, proof.State, code.State)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
