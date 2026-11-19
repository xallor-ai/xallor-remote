package relay

import (
	"context"
	"errors"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

type settingsUnknownCommit struct {
	DeviceStore
	calls  int
	failed bool
}

func (s *settingsUnknownCommit) UpdateDevice(ctx context.Context, id string, record access.Device) (access.Device, error) {
	s.calls++
	next, err := s.DeviceStore.UpdateDevice(ctx, id, record)
	if err == nil && !s.failed {
		s.failed = true
		return access.Device{}, errors.New("fixture lost committed settings reply")
	}
	return next, err
}

// A committed update whose result is lost may leave its new epoch active in
// storage. Do not pretend rollback/inactivity; fence routing, preserve revoked
// consent and require a fresh authenticated registration before new approval.
func TestSessionSettings_shouldFenceUnknownCommit_withoutClaimingDatabaseRollback(t *testing.T) {
	s, h, target, original := approvedSessionFixture(t)
	active := original.last()
	toggleSessionInbound(h, target, false)
	old := h.devices["device"].sessionTarget
	fault := &settingsUnknownCommit{DeviceStore: s}
	h.store = fault
	toggleSessionInbound(h, target, true)
	stored, err := s.LoadDevice(context.Background(), "device")
	if err != nil {
		t.Fatal(err)
	}
	if fault.calls != 1 || !stored.Inbound || !stored.SessionEpochActive || stored.SessionEpoch == old.Epoch {
		t.Fatal("fixture did not produce one acknowledged-lost commit")
	}
	if h.DeviceOnline("device") || h.devices["device"].ready || target.last().Code != protocol.RelayError {
		t.Fatal("unknown commit restored routing or reported success")
	}
	assertRetiredTargetConsent(t, s, active)
	fresh := &recSender{}
	if code := h.RegisterClient("device", "xr_grant_transport", "", fresh); code != "" {
		t.Fatal(code)
	}
	resume := protocol.Message{Type: protocol.TypeSessionResumeRequest, RequestID: "resume", SessionID: active.SessionID, ResumeID: active.ResumeID, ResumeCapability: active.ResumeCapability, TargetUID: old.DeviceUID}
	h.Handle(fresh, protocol.RoleClient, "device", resume)
	if fresh.last().Status == protocol.SessionActive {
		t.Fatal("unknown commit resurrected old consent")
	}
	replacement := &recSender{}
	if code := h.RegisterDevice("device", "secret", true, replacement); code != "" {
		t.Fatal(code)
	}
	toggleSessionInbound(h, replacement, true)
	if !h.DeviceOnline("device") || !h.devices["device"].ready || h.devices["device"].sessionTarget.Epoch == stored.SessionEpoch {
		t.Fatal("registration did not reconcile with fresh epoch")
	}
	h.Handle(fresh, protocol.RoleClient, "device", resume)
	if fresh.last().Status == protocol.SessionActive {
		t.Fatal("reconnect revived old proof")
	}
	request := nextSessionRequest(t, s, h, fresh, "after-unknown", 2)
	approveSession(t, h, replacement, fresh, request)
	if fresh.last().Status != protocol.SessionActive {
		t.Fatal("new explicit consent not usable")
	}
}
