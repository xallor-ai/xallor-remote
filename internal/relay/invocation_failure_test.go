package relay

import (
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/admission"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

type routingFaultSender struct {
	recSender
	failType string
}

func (s *routingFaultSender) Enqueue(m protocol.Message) error {
	if m.Type == s.failType {
		return errOutboxClosed
	}
	return s.recSender.Enqueue(m)
}

// Queue failures retire both indexes. A failed client must cancel all its
// device invocations using internal IDs, never another client's correlation.
func TestRouting_should_retireAllMappings_whenEitherQueueRejects(t *testing.T) {
	for _, side := range []string{"device", "client"} {
		t.Run(side, func(t *testing.T) {
			h := NewHub(newMemStore(), nil, nil, admission.New(admission.Limits{PerDevice: protocol.MaxDeviceRequests}))
			d, c := &routingFaultSender{}, &routingFaultSender{}
			if code := h.RegisterDevice("device", "secret", true, d); code != "" {
				t.Fatal(code)
			}
			h.Handle(d, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeDeviceSettings, RequestID: "settings", Inbound: protocol.Bool(true), GrantHash: protocol.SHA256Hex("grant")})
			if code := h.RegisterClient("device", "grant", "", c); code != "" {
				t.Fatal(code)
			}
			if side == "device" {
				d.failType = protocol.TypeInvoke
			} else {
				c.failType = protocol.TypeStdout
			}
			h.Handle(c, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeInvoke, Op: protocol.OpInfo, ExecID: "one"})
			first := d.last().ExecID
			if side == "client" {
				h.Handle(c, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeInvoke, Op: protocol.OpInfo, ExecID: "two"})
				second := d.last().ExecID
				h.Handle(d, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeStdout, ExecID: first, Data: "fail"})
				cancels := map[string]bool{}
				for _, m := range d.got {
					if m.Op == protocol.OpCancel {
						cancels[m.ExecID] = true
					}
				}
				if !cancels[first] || !cancels[second] || len(h.clients) != 0 {
					t.Fatal("client failure lost cancellation")
				}
			} else if c.last().ExecID != "one" || c.last().Code != protocol.DeviceOffline {
				t.Fatal("device failure lost caller correlation")
			}
			if len(h.requests) != 0 || len(h.inflight) != 0 {
				t.Fatal("queue failure leaked mappings")
			}
			if h.admission.Active(h.devices["device"].record.UID) != 0 {
				t.Fatal("queue failure leaked transport capacity")
			}
		})
	}
}

// Ownership changes remove caller correlation as well as inflight state. The
// same online device cannot deliver its former owner's late result to its new owner.
func TestRouting_should_discardFormerOwnerEvents_whenOwnershipTransfers(t *testing.T) {
	s := openIdentityTestStore(t)
	a, b := enrollment(t, s, "alice", ""), enrollment(t, s, "bob", "")
	h := NewHub(s, s, access.NewService(s), admission.New(admission.Limits{PerDevice: protocol.MaxDeviceRequests}))
	d, old, newOwner := &recSender{}, &recSender{}, &recSender{}
	if code := h.RegisterDevice("device", "secret", true, d); code != "" {
		t.Fatal(code)
	}
	h.Handle(d, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeDeviceSettings, RequestID: "settings", GrantHash: protocol.SHA256Hex("grant"), Inbound: protocol.Bool(true)})
	controlAccess(t, h, d, a.Token, access.Request{Action: "claim"})
	if code := h.RegisterClient("device", "", a.Token, old); code != "" {
		t.Fatal(code)
	}
	first := routeRequest(h, d, old, "same")
	controlAccess(t, h, d, a.Token, access.Request{Action: "transfer", MembershipID: b.MembershipID})
	if len(h.requests) != 0 || old.last().Code != protocol.Unauthorized || old.last().ExecID != "same" {
		t.Fatal("old ownership retained correlation")
	}
	if code := h.RegisterClient("device", "", b.Token, newOwner); code != "" {
		t.Fatal(code)
	}
	second := routeRequest(h, d, newOwner, "same")
	h.Handle(d, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeStdout, ExecID: first, Data: "old-owner"})
	h.Handle(d, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeExit, ExecID: first})
	if newOwner.last().Data == "old-owner" || h.inflight[second] == nil {
		t.Fatal("transfer crossed invocation ownership")
	}
	h.Handle(d, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeExit, ExecID: second})
	if len(h.requests) != 0 || len(h.inflight) != 0 || newOwner.last().ExecID != "same" {
		t.Fatal("new owner result was lost")
	}
}
