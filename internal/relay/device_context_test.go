package relay

import (
	"context"
	"strings"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/admission"
	"github.com/xallor-ai/xallor-remote/internal/invocation"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Confirmations carry only the committed authority/binding. Reconnecting after
// a missed management result must publish the replacement ownership from SQL.
func TestDeviceContext_should_publishCurrentBinding_onSettingsAndOwnershipChanges(t *testing.T) {
	s := openIdentityTestStore(t)
	a, b := enrollment(t, s, "alice", ""), enrollment(t, s, "bob", "")
	h := NewHub(s, s, access.NewService(s), nil, admission.New(admission.Limits{PerDevice: protocol.MaxDeviceRequests}))
	device := &recSender{}
	if code := h.RegisterDevice("device", "secret", true, device); code != "" {
		t.Fatal(code)
	}
	settings := protocol.Message{Type: protocol.TypeDeviceSettings, RequestID: "settings", Inbound: protocol.Bool(true), GrantHash: protocol.SHA256Hex("grant")}
	h.Handle(device, protocol.RoleDevice, "device", settings)
	initial := device.last().DeviceContext
	if initial == nil || !initial.Valid() || initial.Authority != s.Authority() || initial.GrantID == "" || initial.OwnershipID != "" {
		t.Fatal("missing bearer resource context")
	}
	claim := controlAccess(t, h, device, a.Token, access.Request{Action: "claim"})
	claimed := device.last().DeviceContext
	if claimed == nil || !claimed.Valid() || claimed.OwnershipID != claim.OwnershipID || claimed.GrantID != "" {
		t.Fatal("claim did not publish ownership")
	}
	// A commit survives losing its response/connection. The next handshake must
	// reconstruct context from durable authority, never from the old Hub cache.
	record, err := s.LoadDevice(context.Background(), "device")
	if err != nil {
		t.Fatal(err)
	}
	mutation, err := access.NewService(s).Manage(context.Background(), "device", record, a.Identity, access.Request{Action: "transfer", MembershipID: b.MembershipID})
	if err != nil {
		t.Fatal(err)
	}
	fresh := &recSender{}
	if code := h.RegisterDevice("device", "secret", true, fresh); code != "" {
		t.Fatal(code)
	}
	h.Handle(fresh, protocol.RoleDevice, "device", settings)
	reconnected := fresh.last().DeviceContext
	if reconnected == nil || !reconnected.Valid() || reconnected.OwnershipID != mutation.Device.OwnershipID || *reconnected == *claimed || reconnected.DeviceUID != initial.DeviceUID {
		t.Fatal("reconnect restored stale ownership")
	}
	settings.Inbound = protocol.Bool(false)
	h.Handle(fresh, protocol.RoleDevice, "device", settings)
	if current := fresh.last().DeviceContext; current == nil || current.Inbound {
		t.Fatal("disabled inbound not propagated")
	}
}

// Unused/forged resource context is still retained by the inbox before dispatch;
// its strings must consume the same byte budget as other envelope metadata.
func TestDeviceContext_should_countUntrustedMetadata_inInboxBudget(t *testing.T) {
	i := &inbox{frames: make(chan inboundFrame, protocol.MaxRelayInboxFrames)}
	message := protocol.Message{Type: protocol.TypeHeartbeat, DeviceContext: &invocation.DeviceContext{Authority: strings.Repeat("a", protocol.MaxWSMessageBytes)}}
	drop := func(protocol.Message) { t.Fatal("control message was silently dropped") }
	if !i.offer(message, drop) || i.offer(message, drop) {
		t.Fatal("resource context bypassed byte accounting")
	}
}
