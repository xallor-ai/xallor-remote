package relay

import (
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/admission"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Hub must release an injected transport lease only at a terminal event; a
// custom limit proves routing no longer owns a second hidden counter.
func TestHub_should_useInjectedAdmissionLease_whenTerminalArrives(t *testing.T) {
	h := NewHub(nil, nil, nil, admission.New(admission.Limits{PerDevice: 1}))
	device, client := &recSender{}, &recSender{}
	if code := h.RegisterDevice("dev_a", "secret", true, device); code != "" {
		t.Fatal(code)
	}
	authorizeTestClient(t, h, device, client)
	uid := h.devices["dev_a"].record.UID
	h.Handle(client, protocol.RoleClient, "dev_a", protocol.Message{Type: protocol.TypeInvoke, Op: protocol.OpInfo, ExecID: "first"})
	h.Handle(client, protocol.RoleClient, "dev_a", protocol.Message{Type: protocol.TypeInvoke, Op: protocol.OpInfo, ExecID: "second"})
	if client.last().Code != protocol.QuotaExceeded || h.admission.Active(uid) != 1 {
		t.Fatal("injected limit was not enforced")
	}
	first := device.last().ExecID
	h.Handle(device, protocol.RoleDevice, "dev_a", protocol.Message{Type: protocol.TypeExit, ExecID: first})
	if h.admission.Active(uid) != 0 {
		t.Fatal("terminal did not release admission lease")
	}
	h.Handle(client, protocol.RoleClient, "dev_a", protocol.Message{Type: protocol.TypeInvoke, Op: protocol.OpInfo, ExecID: "replacement"})
	if device.last().Op != protocol.OpInfo || len(h.inflight) != 1 || h.admission.Active(uid) != 1 {
		t.Fatal("released admission was not reusable")
	}
}

// Every routing retirement returns its transport slot. Cancellation alone
// keeps it, and a duplicate old terminal cannot free the replacement request.
func TestHub_should_releaseTransportLease_whenRouteRetires(t *testing.T) {
	for _, reason := range []string{protocol.TypeExit, protocol.TypeError, protocol.TypeInvokeNack, "client-drop", "device-drop", "device-replace", "ownership-change"} {
		t.Run(reason, func(t *testing.T) {
			h, device, client, other := routingFixture(t)
			uid := h.devices["device"].record.UID
			first := routeRequest(h, device, client, "first")
			second := routeRequest(h, device, other, "second")
			h.Handle(client, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeInvoke, Op: protocol.OpCancel, ExecID: "first"})
			if h.admission.Active(uid) != 2 {
				t.Fatal("sending cancellation returned capacity prematurely")
			}
			remaining := 0
			switch reason {
			case "client-drop":
				h.Drop(client, protocol.RoleClient, "device")
				remaining = 1
			case "device-drop":
				h.Drop(device, protocol.RoleDevice, "device")
			case "device-replace":
				if code := h.RegisterDevice("device", "secret", true, &recSender{}); code != "" {
					t.Fatal(code)
				}
			case "ownership-change":
				h.mu.Lock()
				h.invalidateOwnershipLocked("device")
				h.mu.Unlock()
			default:
				h.Handle(device, protocol.RoleDevice, "device", protocol.Message{Type: reason, ExecID: first})
				remaining = 1
			}
			if h.admission.Active(uid) != remaining || len(h.inflight) != remaining {
				t.Fatal("retired route leaked or over-released capacity")
			}
			h.Handle(device, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeExit, ExecID: first})
			if h.admission.Active(uid) != remaining {
				t.Fatal("duplicate terminal over-released capacity")
			}
			if remaining == 1 {
				h.Handle(device, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeExit, ExecID: second})
				if h.admission.Active(uid) != 0 {
					t.Fatal("remaining route did not release")
				}
			}
		})
	}
}
