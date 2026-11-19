package noderun

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/accesscode"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

func sessionApprovalFixture() accesscode.SessionRequest {
	return accesscode.SessionRequest{RequestID: "request-1", SessionID: "session-1", TargetUID: "device-1", CodeID: "code-1", Generation: 1, Scope: []string{protocol.OpInfo}}
}

// Session approval uses a separate head and preserves digest binding.
func TestSessionApproval_shouldRequireDigestBoundDecision(t *testing.T) {
	h := newSessionApprovalHub()
	sink := &testSink{ch: make(chan ipc.Frame, 2)}
	h.subscribe(sink)
	done := make(chan struct{})
	var allowed bool
	var code string
	go func() {
		allowed, code = h.ask(context.Background(), sessionApprovalFixture())
		close(done)
	}()
	frame := <-sink.ch
	if frame.Event != "session_approval" {
		t.Fatalf("event=%s", frame.Event)
	}
	var params struct {
		RequestSHA256 string `json:"request_sha256"`
	}
	if err := json.Unmarshal(frame.Params, &params); err != nil || params.RequestSHA256 == "" {
		t.Fatalf("missing digest: %s %v", frame.Params, err)
	}
	if h.respond(frame.ID, "wrong", true) {
		t.Fatal("wrong digest approved session")
	}
	if !h.respond(frame.ID, params.RequestSHA256, true) {
		t.Fatal("valid digest rejected")
	}
	<-done
	if !allowed || code != "" {
		t.Fatalf("allowed=%v code=%s", allowed, code)
	}
}

func TestSessionApproval_shouldCancelWhenLastHeadDisconnects(t *testing.T) {
	h := newSessionApprovalHub()
	sink := &testSink{ch: make(chan ipc.Frame, 1)}
	h.subscribe(sink)
	done := make(chan string, 1)
	go func() {
		_, code := h.ask(context.Background(), sessionApprovalFixture())
		done <- code
	}()
	<-sink.ch
	h.unsubscribe(sink)
	select {
	case code := <-done:
		if code != protocol.Cancelled {
			t.Fatalf("code=%s", code)
		}
	case <-time.After(time.Second):
		t.Fatal("approval did not cancel after last head disconnected")
	}
}
