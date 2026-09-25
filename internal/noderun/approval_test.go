package noderun

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

type testSink struct {
	ch chan ipc.Frame
}

func (s *testSink) WriteFrame(f ipc.Frame) error {
	select {
	case s.ch <- f:
	default:
	}
	return nil
}

// should_policy_deny_when_no_subscriber: no UI → immediate deny, no wait.
func TestShouldPolicyDenyWhenNoSubscriber(t *testing.T) {
	h := newApprovalHub()
	start := time.Now()
	allow, code := h.ask("ex_1", "shutdown now")
	if allow || code != protocol.PolicyDeny {
		t.Fatalf("allow=%v code=%s", allow, code)
	}
	if time.Since(start) > time.Second {
		t.Fatal("should not wait when no subscriber")
	}
}

// should_allow_when_subscriber_responds_true
func TestShouldAllowWhenSubscriberRespondsTrue(t *testing.T) {
	h := newApprovalHub()
	s := &testSink{ch: make(chan ipc.Frame, 2)}
	h.subscribe(s)
	done := make(chan struct{})
	var allow bool
	var code string
	go func() {
		allow, code = h.ask("ex_2", "Stop-Computer")
		close(done)
	}()
	fr := <-s.ch
	if fr.Event != "approval" {
		t.Fatalf("event=%s", fr.Event)
	}
	var p struct {
		ExecID string `json:"exec_id"`
	}
	_ = json.Unmarshal(fr.Params, &p)
	if p.ExecID != "ex_2" {
		t.Fatalf("exec_id=%s", p.ExecID)
	}
	if !h.respond(fr.ID, true) {
		t.Fatal("respond failed")
	}
	<-done
	if !allow || code != "" {
		t.Fatalf("allow=%v code=%s", allow, code)
	}
}

// should_hard_deny_catastrophic_not_approval
func TestShouldHardDenyCatastrophic(t *testing.T) {
	if !hardDenyCommand("rm -rf /") {
		t.Fatal("rm -rf /")
	}
	if needsApprovalCommand("rm -rf /") {
		t.Fatal("rm -rf / should not be approval-only")
	}
}

// should_need_approval_for_shutdown_family
func TestShouldNeedApprovalForShutdown(t *testing.T) {
	if osName() == "windows" {
		if hardDenyCommand("Stop-Computer") {
			t.Fatal("Stop-Computer should not be hard deny")
		}
		if !needsApprovalCommand("Stop-Computer") {
			t.Fatal("Stop-Computer")
		}
		return
	}
	if !needsApprovalCommand("sudo ls") {
		t.Fatal("sudo")
	}
	if !needsApprovalCommand("reboot") {
		t.Fatal("reboot")
	}
}
