package relay

import (
	"strings"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/admission"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Output overflow is dropped while retaining ordered capacity for terminal/control messages.
func TestInbox_should_reserveTerminalCapacity_whenOutputFloods(t *testing.T) {
	i := &inbox{frames: make(chan inboundFrame, protocol.MaxRelayInboxFrames)}
	dropped := 0
	drop := func(protocol.Message) { dropped++ }
	for n := 0; n < protocol.MaxRelayInboxFrames*2; n++ {
		if !i.offer(protocol.Message{Type: protocol.TypeStdout, ExecID: "exec", Data: "output"}, drop) {
			t.Fatal("output closed shared connection")
		}
	}
	if dropped == 0 {
		t.Fatal("fixture did not fill output slots")
	}
	if !i.offer(protocol.Message{Type: protocol.TypeExit, ExecID: "exec"}, drop) {
		t.Fatal("lost terminal")
	}
	for len(i.frames) != 0 {
		frame := <-i.frames
		i.bytes.Add(-frame.bytes)
		if frame.message.Type == protocol.TypeExit && len(i.frames) != 0 {
			t.Fatal("terminal reordered")
		}
	}
	if i.bytes.Load() != 0 {
		t.Fatal("byte accounting leaked")
	}
}

// Control floods and large unused fields are bounded independently of output truncation.
func TestInbox_should_rejectControlOverload_and_countMetadata(t *testing.T) {
	i := &inbox{frames: make(chan inboundFrame, protocol.MaxRelayInboxFrames)}
	drop := func(protocol.Message) { t.Fatal("control was silently discarded") }
	m := protocol.Message{Type: protocol.TypeHeartbeat, Workspace: strings.Repeat("x", protocol.MaxWSMessageBytes)}
	if !i.offer(m, drop) {
		t.Fatal("first bounded message rejected")
	}
	if i.offer(m, drop) {
		t.Fatal("accepted beyond byte budget")
	}
	if i.bytes.Load() > protocol.MaxRelayInboxBytes {
		t.Fatal("byte budget exceeded")
	}
	j := &inbox{frames: make(chan inboundFrame, protocol.MaxRelayInboxFrames)}
	for n := 0; n < protocol.MaxRelayInboxFrames; n++ {
		if !j.offer(protocol.Message{Type: protocol.TypeHeartbeat}, drop) {
			t.Fatal("early overflow")
		}
	}
	if j.offer(protocol.Message{Type: protocol.TypeHeartbeat}, drop) {
		t.Fatal("accepted beyond frame budget")
	}
}

// A source connection cannot mark another device's execution as truncated.
func TestInbox_should_requireOwningDevice_whenMarkingDroppedOutput(t *testing.T) {
	h := NewHub(nil, nil, nil, admission.New(admission.Limits{PerDevice: protocol.MaxDeviceRequests}))
	a, b, client := &recSender{}, &recSender{}, &recSender{}
	h.RegisterDevice("dev_a", "a", true, a)
	h.RegisterDevice("dev_b", "b", true, b)
	authorizeTestClient(t, h, a, client)
	h.Handle(client, protocol.RoleClient, "dev_a", protocol.Message{Type: protocol.TypeInvoke, ExecID: "owned", Op: protocol.OpExec})
	message := protocol.Message{Type: protocol.TypeStdout, ExecID: a.last().ExecID}
	h.noteDroppedOutput(b, message)
	if h.inflight[message.ExecID].trunc {
		t.Fatal("other device changed output state")
	}
	h.noteDroppedOutput(a, message)
	if !h.inflight[message.ExecID].trunc {
		t.Fatal("owner's dropped output unreported")
	}
}
