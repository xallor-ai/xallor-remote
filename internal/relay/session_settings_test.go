package relay

import (
	"context"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/accesscode"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Turning inbound off and on is an authorization boundary even if the target
// socket and sharing credential stay unchanged. Retained consent cannot revive.
func TestSessionSettings_shouldRevokeRetainedConsent_whenInboundCycles(t *testing.T) {
	for _, connected := range []bool{false, true} {
		name := "disconnected-controller"
		if connected {
			name = "connected-controller"
		}
		t.Run(name, func(t *testing.T) {
			s, h, target, client := approvedSessionFixture(t)
			active := client.last()
			old := h.devices["device"].sessionTarget
			if !connected {
				h.Drop(client, protocol.RoleClient, "device")
			}
			for _, on := range []bool{false, true} {
				h.Handle(target, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeDeviceSettings, RequestID: "toggle", GrantHash: protocol.SHA256Hex("xr_grant_transport"), Inbound: protocol.Bool(on)})
				if target.last().Code != "" {
					t.Fatal("settings failed")
				}
				assertRetiredTargetConsent(t, s, active)
			}
			if !h.DeviceOnline("device") || h.devices["device"].conn != target {
				t.Error("settings cycle replaced target transport")
			}
			if old == h.devices["device"].sessionTarget {
				t.Error("re-enable resurrected old epoch")
			}
			fresh := &recSender{}
			if code := h.RegisterClient("device", "xr_grant_transport", "", fresh); code != "" {
				t.Fatal(code)
			}
			h.Handle(fresh, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeSessionResumeRequest, RequestID: "old-resume", SessionID: active.SessionID, ResumeID: active.ResumeID, ResumeCapability: active.ResumeCapability, TargetUID: old.DeviceUID})
			if fresh.last().Status == protocol.SessionActive {
				t.Error("old consent resumed after inbound cycle")
			}
		})
	}
}

// Pending recipient approval is invalidated by inbound-off; a delayed approval
// after re-enable must neither consume its code nor become an active session.
func TestSessionSettings_shouldInvalidatePendingApproval_whenInboundCycles(t *testing.T) {
	s, h, target, _ := approvedSessionFixture(t)
	client := &recSender{}
	request := nextSessionRequest(t, s, h, client, "pending-switch", 2)
	h.Handle(client, protocol.RoleClient, "device", request)
	approval := target.last()
	for _, on := range []bool{false, true} {
		h.Handle(target, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeDeviceSettings, RequestID: "toggle", GrantHash: protocol.SHA256Hex("xr_grant_transport"), Inbound: protocol.Bool(on)})
	}
	h.Handle(target, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeSessionApprovalResult, RequestID: approval.RequestID, SessionID: approval.SessionID, RequestSHA256: approval.RequestSHA256, Allow: protocol.Bool(true)})
	if got := client.last(); got.Status == protocol.SessionActive {
		t.Error("old pending approval revived")
	}
	if err := s.ReadAccessCode(context.Background(), func(r accesscode.Reader) error {
		p, err := r.Reservation(context.Background(), request.RequestID)
		if err != nil {
			return err
		}
		if p.State != accesscode.ReservationUnknown {
			t.Errorf("pending outcome=%s", p.State)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
