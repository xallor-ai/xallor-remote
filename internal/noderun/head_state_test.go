package noderun

import (
	"context"
	"encoding/json"
	"github.com/xallor-ai/xallor-remote/internal/identity"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
	"net"
	"testing"
	"time"
)

// A separate request ID must address an approval explicitly and can decide it only once.
func TestHead_should_requireExplicitApprovalAndName(t *testing.T) {
	store, err := identity.LoadFrom(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = store.AddPeer("123456789", "fixture-grant"); err != nil {
		t.Fatal(err)
	}
	if err = store.RenamePeer("123456789", "unchanged"); err != nil {
		t.Fatal(err)
	}
	d := New(store, nil)
	defer d.Stop()
	d.approvals.pending["a1"] = &pendingApproval{execID: "exec", ch: make(chan decide, 1)}
	a, b := net.Pipe()
	defer b.Close()
	_ = b.SetDeadline(time.Now().Add(3 * time.Second))
	go d.serveIPC(ipc.NewConn(a))
	client := ipc.NewConn(b)
	cases := []struct {
		method, params string
		ok             bool
	}{
		{"peer.rename", `{"device_id":"123456789"}`, false},
		{"approval.respond", `{"allow":true}`, false},
		{"approval.respond", `{"approval_id":"a1","allow":true}`, true},
		{"approval.respond", `{"approval_id":"a1","allow":false}`, false},
		{"approval.list", `{}`, true},
		{"audit.list", `{}`, true},
	}
	for _, c := range cases {
		if err = ipc.Call(client, "independent-id", c.method, json.RawMessage(c.params)); err != nil {
			t.Fatal(err)
		}
		frame, e := client.ReadFrame()
		if e != nil || frame.OK == nil || *frame.OK != c.ok {
			t.Fatalf("%s %+v %v", c.method, frame, e)
		}
	}
	peer, _ := store.Peer("123456789")
	if peer.Name != "unchanged" {
		t.Fatal("missing name erased alias")
	}
}

// A cancellation before the approval goroutine starts must not leave a 60s prompt.
func TestApproval_should_cancelBeforeOrDuringAsk(t *testing.T) {
	for _, before := range []bool{true, false} {
		h := newApprovalHub()
		sink := &testSink{ch: make(chan ipc.Frame, 1)}
		h.subscribe(sink)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		if before {
			cancel()
		}
		done := make(chan string, 1)
		go func() { _, code := h.ask(ctx, "exec", "echo harmless"); done <- code }()
		if !before {
			select {
			case <-sink.ch:
			case <-time.After(time.Second):
				t.Fatal("no prompt")
			}
			cancel()
		}
		select {
		case code := <-done:
			if code != protocol.Cancelled {
				t.Fatal(code)
			}
		case <-time.After(time.Second):
			t.Fatal("cancel stuck")
		}
		if len(h.list()) != 0 {
			t.Fatal("stale approval")
		}
	}
}
