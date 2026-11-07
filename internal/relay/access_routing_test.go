package relay

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/admission"
	"github.com/xallor-ai/xallor-remote/internal/principal"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

func controlAccess(t *testing.T, h *Hub, device *recSender, token string, request access.Request) access.State {
	t.Helper()
	raw, _ := json.Marshal(request)
	h.Handle(device, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeAccessRequest, RequestID: "operation", AccountToken: token, Payload: raw})
	msg := device.last()
	if msg.Type != protocol.TypeAccessResult || msg.Code != "" {
		t.Fatal("management", msg.Type, msg.Code)
	}
	var result access.State
	if err := json.Unmarshal(msg.Payload, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

// Claim requires the live device connection; a controller cannot elevate even
// with a valid account token and bearer. Claiming invalidates existing clients.
func TestAccess_should_rejectControllerClaimAndInvalidateBearer_whenDeviceClaims(t *testing.T) {
	s := openIdentityTestStore(t)
	a := enrollment(t, s, "alice", "")
	b := enrollment(t, s, "bob", "")
	h := NewHub(s, s, access.NewService(s), nil, admission.New(admission.Limits{PerDevice: protocol.MaxDeviceRequests}))
	device, controller := &recSender{}, &recSender{}
	if code := h.RegisterDevice("device", "secret", true, device); code != "" {
		t.Fatal(code)
	}
	h.Handle(device, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeDeviceSettings, RequestID: "settings", Inbound: protocol.Bool(true), GrantHash: protocol.SHA256Hex("grant")})
	if code := h.RegisterClient("device", "grant", b.Token, controller); code != "" {
		t.Fatal(code)
	}
	h.Handle(controller, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeAccessRequest, RequestID: "claim", AccountToken: b.Token, Payload: json.RawMessage(`{"action":"claim"}`)})
	if controller.last().Code != protocol.Unauthorized {
		t.Fatal("controller claimed device")
	}
	h.Handle(controller, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeInvoke, Op: protocol.OpInfo, ExecID: "old"})
	controlAccess(t, h, device, a.Token, access.Request{Action: "claim"})
	if len(h.inflight) != 0 || len(h.clients) != 0 {
		t.Fatal("claim retained old channel ownership")
	}
	for _, token := range []string{"", b.Token} {
		if code := h.RegisterClient("device", "grant", token, &recSender{}); code != protocol.Unauthorized {
			t.Fatal("bearer bypassed ownership", code)
		}
	}
	owner := &recSender{}
	if code := h.RegisterClient("device", "", a.Token, owner); code != "" {
		t.Fatal("owner required a bearer", code)
	}
	controlAccess(t, h, device, a.Token, access.Request{Action: "share", MembershipID: b.MembershipID, Operations: []string{"info"}, TTLSeconds: 60})
	guest := &recSender{}
	if code := h.RegisterClient("device", "", b.Token, guest); code != "" {
		t.Fatal(code)
	}
	h.Handle(guest, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeInvoke, Op: protocol.OpExec, ExecID: "forbidden"})
	if guest.last().Code != protocol.Unauthorized || h.requests[requestKey{client: guest, id: "forbidden"}] != "" {
		t.Fatal("operation permission bypassed")
	}
	h.Handle(guest, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeInvoke, Op: protocol.OpInfo, ExecID: "allowed"})
	if device.last().Type != protocol.TypeInvoke || device.last().ExecID != h.requests[requestKey{client: guest, id: "allowed"}] {
		t.Fatal("shared operation was not routed")
	}
	before, _ := s.LoadDevice(context.Background(), "device")
	controlAccess(t, h, device, a.Token, access.Request{Action: "transfer", MembershipID: b.MembershipID})
	controlAccess(t, h, device, b.Token, access.Request{Action: "transfer", MembershipID: a.MembershipID})
	h.Handle(owner, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeInvoke, Op: protocol.OpInfo, ExecID: "resurrected"})
	if owner.last().Code != protocol.Unauthorized || h.requests[requestKey{client: owner, id: "resurrected"}] != "" {
		t.Fatal("transfer back revived old owner")
	}
	after, _ := s.LoadDevice(context.Background(), "device")
	if before.OwnershipID == after.OwnershipID {
		t.Fatal("ownership generation reused")
	}
}

// Independent direct transactions racing to claim one unclaimed incarnation
// must commit exactly one owner, never last-writer-wins ownership replacement.
func TestAccess_should_commitOneOwner_whenClaimsRace(t *testing.T) {
	s := openIdentityTestStore(t)
	a := enrollment(t, s, "alice", "")
	b := enrollment(t, s, "bob", "")
	ctx, cancel := context.WithTimeout(context.Background(), protocol.RelayIdentityTimeout)
	defer cancel()
	device, err := s.RegisterDevice(ctx, "device", "secret", true)
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	start := make(chan struct{})
	for _, actor := range []principal.Identity{a.Identity, b.Identity} {
		go func() {
			<-start
			_, err := access.NewService(s).Manage(ctx, "device", device, actor, access.Request{Action: "claim"})
			results <- err
		}()
	}
	close(start)
	wins := 0
	for range 2 {
		if err := <-results; err == nil {
			wins++
		} else if !errors.Is(err, access.ErrDenied) {
			t.Fatal("claim failed operationally instead of resolving ownership", err)
		}
	}
	if wins != 1 {
		t.Fatal("claim winners", wins)
	}
}
