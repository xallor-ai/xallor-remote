package relay

import (
	"context"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/admission"
	"github.com/xallor-ai/xallor-remote/internal/delegation"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

func executionRouteFixture(t *testing.T) (*Hub, *recSender, *recSender, string) {
	t.Helper()
	s := newMemStore()
	h := NewHub(s, nil, nil, delegation.NewAuthority([32]byte{1}, s, nil, nil), admission.New(admission.Limits{PerDevice: protocol.MaxDeviceRequests}))
	device, caller := &recSender{}, &recSender{}
	if code := h.RegisterDevice("device", "secret", true, device); code != "" {
		t.Fatal(code)
	}
	h.Handle(device, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeDeviceSettings, RequestID: "settings", Inbound: protocol.Bool(true), GrantHash: protocol.SHA256Hex("grant")})
	if code := h.RegisterClient("device", "grant", "", caller); code != "" {
		t.Fatal(code)
	}
	h.Handle(caller, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeInvoke, Op: protocol.OpExec, ExecID: "request"})
	return h, device, caller, device.last().ExecID
}

// Only the original target can renew an active exec route; request ID reuse,
// completion, cancellation and other operation kinds never extend authority.
func TestExecutionLease_should_bindRenewalToLiveRouteAndDevice(t *testing.T) {
	for _, kind := range []string{"live", "caller", "other-device", "finished", "cancelled", "other-op", "reused", "forged-proof"} {
		t.Run(kind, func(t *testing.T) {
			h, device, caller, id := executionRouteFixture(t)
			request := protocol.Message{Type: protocol.TypeExecutionRenewRoute, RequestID: "renew", ExecID: id}
			from, role := device, protocol.RoleDevice
			switch kind {
			case "caller":
				from, role = caller, protocol.RoleClient
			case "other-device":
				from = &recSender{}
			case "finished", "reused":
				h.Handle(device, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeExit, ExecID: id})
				if kind == "reused" {
					h.Handle(caller, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeInvoke, Op: protocol.OpExec, ExecID: "request"})
				}
			case "cancelled":
				h.Handle(caller, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeInvoke, Op: protocol.OpCancel, ExecID: "request"})
			case "other-op":
				h.inflight[id].operation = protocol.OpInfo
			case "forged-proof":
				request.Delegation = "proof"
			}
			h.Handle(from, role, "device", request)
			reply := from.last()
			if reply.Type != protocol.TypeExecutionLease || reply.RequestID != "renew" {
				t.Fatal(reply)
			}
			if kind == "live" {
				if reply.Code != "" || reply.LeaseMS != protocol.ExecutionLeaseDuration.Milliseconds() {
					t.Fatal(reply)
				}
			} else if reply.Code != protocol.Unauthorized || reply.LeaseMS != 0 {
				t.Fatal(reply)
			}
		})
	}
}

type waitingExecutionAuthority struct {
	ExecutionAuthority
	entered, resume chan struct{}
}

func (a waitingExecutionAuthority) RenewForeground(ctx context.Context, _ delegation.Invocation) (time.Duration, error) {
	close(a.entered)
	select {
	case <-a.resume:
		return protocol.ExecutionLeaseDuration, nil
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

// A storage wait does not hold the router mutex, and its late success cannot
// renew a cancelled or completed route, even while the device stays connected.
func TestExecutionLease_should_rejectLateSuccess_afterConcurrentRouteEnd(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		h, device, caller, id := executionRouteFixture(t)
		waiting := waitingExecutionAuthority{h.executionAuthority, make(chan struct{}), make(chan struct{})}
		h.executionAuthority = waiting
		done := make(chan struct{})
		go func() {
			defer close(done)
			h.Handle(device, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeExecutionRenewRoute, RequestID: "renew", ExecID: id})
		}()
		select {
		case <-waiting.entered:
		case <-time.After(time.Second):
			t.Fatal("renewal not started")
		}
		if cancel {
			h.handleCancel(caller, "request")
		} else {
			h.mu.Lock()
			h.finishInvocationLocked(id)
			h.mu.Unlock()
		}
		close(waiting.resume)
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("renewal did not finish")
		}
		if reply := device.last(); reply.Code != protocol.Unauthorized || reply.LeaseMS != 0 {
			t.Fatal(reply)
		}
	}
}
