package relay

import (
	"context"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

type cancelAfterCommitStore struct {
	DeviceStore
	cancel context.CancelFunc
}

func (s *cancelAfterCommitStore) UpdateDevice(ctx context.Context, id string, record access.Device) (access.Device, error) {
	committed, err := s.DeviceStore.UpdateDevice(ctx, id, record)
	if err != nil {
		return access.Device{}, err
	}
	s.cancel()
	return committed, nil
}

// A commit that wins the race with cancellation must still invalidate the old cached grant.
func TestIdentity_should_publishCommittedGrant_whenCancellationFollowsCommit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := &cancelAfterCommitStore{DeviceStore: newMemStore(), cancel: cancel}
	h := NewHub(s)
	device := &contextSender{ctx: ctx}
	if code := h.RegisterDevice("device", "secret", true, device); code != "" {
		t.Fatal(code)
	}
	h.Handle(device, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeDeviceSettings, RequestID: "settings", Inbound: protocol.Bool(true), GrantHash: protocol.SHA256Hex("committed")})
	if ctx.Err() == nil {
		t.Fatal("fixture did not cancel after commit")
	}
	if h.devices["device"].record.GrantHash != protocol.SHA256Hex("committed") {
		t.Fatal("committed change was lost on cancellation")
	}
	h.Drop(device, protocol.RoleDevice, "device")
	if code := h.RegisterClient("device", "committed", "", &recSender{}); code != "" {
		t.Fatal(code)
	}
}
