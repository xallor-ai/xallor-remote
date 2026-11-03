package relay

import (
	"fmt"
	"strings"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Pending requests share a device transport budget; cancellation needs no new slot.
func TestHub_should_boundRequests_and_allowCancel_whenTransportFull(t *testing.T) {
	h := NewHub(nil, nil, nil)
	device, client, other := &recSender{}, &recSender{}, &recSender{}
	h.RegisterDevice("dev_a", "secret", true, device)
	authorizeTestClient(t, h, device, client)
	if code := h.RegisterClient("dev_a", "test", "", other); code != "" {
		t.Fatal(code)
	}
	var first string
	for n := 0; n < protocol.MaxDeviceRequests; n++ {
		h.Handle(client, protocol.RoleClient, "dev_a", protocol.Message{Type: protocol.TypeInvoke, ExecID: fmt.Sprint(n), Op: protocol.OpInfo})
		if len(h.inflight) != n+1 {
			t.Fatalf("request %d not admitted", n)
		}
		if n == 0 {
			first = device.last().ExecID
		}
	}
	h.Handle(other, protocol.RoleClient, "dev_a", protocol.Message{Type: protocol.TypeInvoke, ExecID: "overflow", Op: protocol.OpExec})
	if other.last().Code != protocol.QuotaExceeded || len(h.inflight) != protocol.MaxDeviceRequests {
		t.Fatal("transport cap was not shared across clients")
	}
	h.Handle(client, protocol.RoleClient, "dev_a", protocol.Message{Type: protocol.TypeInvoke, ExecID: "0", Op: protocol.OpCancel})
	if m := device.last(); m.Op != protocol.OpCancel || m.ExecID != first {
		t.Fatal("cancel could not reach full device")
	}
	if len(h.inflight) != protocol.MaxDeviceRequests {
		t.Fatal("cancel prematurely released request")
	}
	h.Handle(device, protocol.RoleDevice, "dev_a", protocol.Message{Type: protocol.TypeExit, ExecID: first, Status: protocol.ExitCancelled})
	h.Handle(other, protocol.RoleClient, "dev_a", protocol.Message{Type: protocol.TypeInvoke, ExecID: "replacement", Op: protocol.OpInfo})
	if device.last().Op != protocol.OpInfo || len(h.inflight) != protocol.MaxDeviceRequests {
		t.Fatal("terminal did not reclaim request capacity")
	}
}

// Flooding output must leave two small control messages per admitted request;
// test both frame pressure and byte pressure without a concurrent consumer.
func TestInbox_should_reserveControlBudget_whenOutputFillsFramesOrBytes(t *testing.T) {
	for _, size := range []int{0, protocol.MaxRelayInboxBytes / 4} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			i := &inbox{frames: make(chan inboundFrame, protocol.MaxRelayInboxFrames)}
			dropped := 0
			output := protocol.Message{Type: protocol.TypeStdout, Data: "x", Workspace: strings.Repeat("x", size)}
			for n := 0; n < protocol.MaxRelayInboxFrames*4; n++ {
				if !i.offer(output, func(protocol.Message) { dropped++ }) {
					t.Fatal("output closed inbox")
				}
			}
			if dropped == 0 {
				t.Fatal("fixture did not exhaust output budget")
			}
			for n := 0; n < protocol.RelayControlReserveFrames; n++ {
				if !i.offer(protocol.Message{Type: protocol.TypeExit, ExecID: fmt.Sprint(n)}, func(protocol.Message) { t.Fatal("control dropped") }) {
					t.Fatal("reserved control rejected")
				}
			}
			if i.bytes.Load() > protocol.MaxRelayInboxBytes {
				t.Fatal("byte budget exceeded")
			}
		})
	}
}

// A stalled socket retains bounded memory and space for every request's control
// messages even when many output streams together exhaust the queue budget.
func TestOutbox_should_reserveControlBudget_whenOutputFillsFramesOrBytes(t *testing.T) {
	for _, size := range []int{1, protocol.MaxFrameBytes} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			b, entered, release, written := blockedOutbox(t, protocol.TypeHelloOK)
			if err := b.Enqueue(protocol.Message{Type: protocol.TypeHelloOK}); err != nil {
				t.Fatal(err)
			}
			waitSignal(t, entered)
			dropped := 0
			for n := 0; n < protocol.MaxRelayQueueFrames*4; n++ {
				err := b.Enqueue(protocol.Message{Type: protocol.TypeStdout, ExecID: fmt.Sprint(n % protocol.MaxDeviceRequests), Data: strings.Repeat("x", size)})
				if err == errOutputDropped {
					dropped++
				} else if err != nil {
					t.Fatal(err)
				}
			}
			if dropped == 0 {
				t.Fatal("fixture did not exhaust output budget")
			}
			for n := 0; n < protocol.RelayControlReserveFrames; n++ {
				if err := b.Enqueue(protocol.Message{Type: protocol.TypeExit, ExecID: fmt.Sprint(n)}); err != nil {
					t.Fatal(err)
				}
			}
			b.mu.Lock()
			total := b.bytes
			b.mu.Unlock()
			if total > protocol.MaxRelayQueueBytes {
				t.Fatal("byte budget exceeded")
			}
			close(release)
			b.shutdown()
			terminals := 0
			for len(written) > 0 {
				if (<-written).Type == protocol.TypeExit {
					terminals++
				}
			}
			if terminals != protocol.RelayControlReserveFrames {
				t.Fatal("lost reserved control frames")
			}
		})
	}
}
