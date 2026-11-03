package relay

import (
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/admission"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// A disconnected caller's delayed output must never be delivered to another
// authenticated subject that reuses its client-selected request correlation ID.
func TestRouting_should_isolateLateOutput_whenAnotherSubjectReusesExecID(t *testing.T) {
	s := openIdentityTestStore(t)
	a, b := enrollment(t, s, "alice", ""), enrollment(t, s, "bob", "")
	h := NewHub(s, s, access.NewService(s), admission.New(admission.Limits{PerDevice: protocol.MaxDeviceRequests}))
	device, alice, bob := &recSender{}, &recSender{}, &recSender{}
	if code := h.RegisterDevice("device", "secret", true, device); code != "" {
		t.Fatal(code)
	}
	h.Handle(device, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeDeviceSettings, RequestID: "settings", GrantHash: protocol.SHA256Hex("grant"), Inbound: protocol.Bool(true)})
	controlAccess(t, h, device, a.Token, access.Request{Action: "claim"})
	controlAccess(t, h, device, a.Token, access.Request{Action: "share", MembershipID: b.MembershipID, Operations: []string{protocol.OpHandoffGet}, TTLSeconds: 3600})
	if code := h.RegisterClient("device", "", a.Token, alice); code != "" {
		t.Fatal(code)
	}
	if code := h.RegisterClient("device", "", b.Token, bob); code != "" {
		t.Fatal(code)
	}
	h.Handle(alice, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeInvoke, Op: protocol.OpHandoffGet, ExecID: "reused-id"})
	previous := device.last()
	if previous.Scope == nil || previous.Scope.PrincipalID != a.PrincipalID {
		t.Fatal("Alice was not dispatched")
	}
	h.Drop(alice, protocol.RoleClient, "device")
	h.Handle(bob, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeInvoke, Op: protocol.OpHandoffGet, ExecID: "reused-id"})
	if current := device.last(); current.Scope == nil || current.Scope.PrincipalID != b.PrincipalID {
		t.Fatal("Bob was not dispatched")
	}
	h.Handle(device, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeStdout, ExecID: previous.ExecID, Data: "alice-private-receipt"})
	if got := bob.last(); got.Data == "alice-private-receipt" {
		t.Fatal("Alice's delayed receipt was delivered to Bob")
	}
}

func routingFixture(t *testing.T) (*Hub, *recSender, *recSender, *recSender) {
	t.Helper()
	h := NewHub(newMemStore(), nil, nil, admission.New(admission.Limits{PerDevice: protocol.MaxDeviceRequests}))
	device, a, b := &recSender{}, &recSender{}, &recSender{}
	if code := h.RegisterDevice("device", "secret", true, device); code != "" {
		t.Fatal(code)
	}
	h.Handle(device, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeDeviceSettings, RequestID: "settings", Inbound: protocol.Bool(true), GrantHash: protocol.SHA256Hex("grant")})
	for _, client := range []*recSender{a, b} {
		if code := h.RegisterClient("device", "grant", "", client); code != "" {
			t.Fatal(code)
		}
	}
	return h, device, a, b
}

func routeRequest(h *Hub, device *recSender, client Sender, id string) string {
	h.Handle(client, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeInvoke, Op: protocol.OpInfo, ExecID: id})
	return device.last().ExecID
}

// Two live clients may use the same correlation ID. Cancellation resolves in
// the caller's connection, and a guessed internal ID is not a cancellation key.
func TestRouting_should_isolateConcurrentIDsAndCancellation(t *testing.T) {
	h, d, a, b := routingFixture(t)
	first, second := routeRequest(h, d, a, "same"), routeRequest(h, d, b, "same")
	if first == second || first == "same" || second == "same" || len(h.inflight) != 2 || len(h.requests) != 2 {
		t.Fatal("routing identities collapsed")
	}
	h.Handle(a, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeInvoke, Op: protocol.OpCancel, ExecID: second})
	if a.last().Code != protocol.UnknownExec {
		t.Fatal("internal ID granted cancel access")
	}
	h.Handle(a, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeInvoke, Op: protocol.OpCancel, ExecID: "same"})
	if d.last().Op != protocol.OpCancel || d.last().ExecID != first {
		t.Fatal("cancel crossed caller boundary")
	}
	h.Drop(a, protocol.RoleClient, "device")
	h.Handle(a, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeInvoke, Op: protocol.OpCancel, ExecID: "same"})
	if a.last().Code != protocol.UnknownExec || len(h.inflight) != 1 || h.requests[requestKey{client: b, id: "same"}] != second {
		t.Fatal("dropped caller affected replacement")
	}
	for _, typ := range []string{protocol.TypeStdout, protocol.TypeStderr, protocol.TypeExit, protocol.TypeError, protocol.TypeInvokeNack} {
		h.Handle(d, protocol.RoleDevice, "device", protocol.Message{Type: typ, ExecID: first, Data: "stale"})
		if b.last().Data == "stale" || len(h.inflight) != 1 {
			t.Fatal("stale event changed current invocation", typ)
		}
	}
	h.Handle(d, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeStdout, ExecID: second, Data: "current"})
	if b.last().ExecID != "same" || b.last().Data != "current" {
		t.Fatal("client correlation was not restored")
	}
	h.Handle(d, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeExit, ExecID: second})
	if b.last().Type != protocol.TypeExit || b.last().ExecID != "same" || len(h.inflight) != 0 || len(h.requests) != 0 {
		t.Fatal("terminal leaked routing state")
	}
}

// Every terminal releases both indexes. Reusing an ID after completion creates
// a distinct device invocation; late terminal/output cannot finish its successor.
func TestRouting_should_releaseBothIndexesAndDiscardLateEvents_afterEveryTerminal(t *testing.T) {
	for _, typ := range []string{protocol.TypeExit, protocol.TypeError, protocol.TypeInvokeNack} {
		t.Run(typ, func(t *testing.T) {
			h, d, a, _ := routingFixture(t)
			first := routeRequest(h, d, a, "same")
			h.Handle(d, protocol.RoleDevice, "device", protocol.Message{Type: typ, ExecID: first})
			if len(h.inflight) != 0 || len(h.requests) != 0 || a.last().ExecID != "same" {
				t.Fatal("terminal leaked correlation")
			}
			second := routeRequest(h, d, a, "same")
			if first == second {
				t.Fatal("internal ID reused")
			}
			h.Handle(d, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeStdout, ExecID: first, Data: "stale"})
			h.Handle(d, protocol.RoleDevice, "device", protocol.Message{Type: typ, ExecID: first})
			if h.inflight[second] == nil || len(h.requests) != 1 || a.last().Data == "stale" {
				t.Fatal("old event reached successor")
			}
			// Duplicate while active must not replace the existing invocation.
			routeRequest(h, d, a, "same")
			if a.last().Code != protocol.AgentError || h.requests[requestKey{client: a, id: "same"}] != second || len(h.inflight) != 1 {
				t.Fatal("active duplicate replaced routing")
			}
		})
	}
}

// Replacing a device connection fails the old request with its caller ID and
// frees the caller key. Neither the old connection nor old route can respond.
func TestRouting_should_rejectOldDeviceAndRoute_afterDeviceReconnect(t *testing.T) {
	h, old, client, _ := routingFixture(t)
	first := routeRequest(h, old, client, "same")
	fresh := &recSender{}
	if code := h.RegisterDevice("device", "secret", true, fresh); code != "" {
		t.Fatal(code)
	}
	if client.last().Code != protocol.DeviceOffline || client.last().ExecID != "same" || len(h.requests) != 0 {
		t.Fatal("replacement did not retire old request")
	}
	h.Handle(fresh, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeDeviceSettings, RequestID: "settings", GrantHash: protocol.SHA256Hex("grant"), Inbound: protocol.Bool(true)})
	second := routeRequest(h, fresh, client, "same")
	for _, source := range []*recSender{old, fresh} {
		h.Handle(source, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeStdout, ExecID: first, Data: "stale"})
	}
	h.Handle(old, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeExit, ExecID: second})
	if client.last().Data == "stale" || h.inflight[second] == nil {
		t.Fatal("replaced device affected new request")
	}
	h.Drop(fresh, protocol.RoleDevice, "device")
	if len(h.requests) != 0 || len(h.inflight) != 0 || client.last().ExecID != "same" {
		t.Fatal("device drop leaked mapping")
	}
}
