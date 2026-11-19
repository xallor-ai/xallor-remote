package noderun

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/execution"
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

func (s *testSink) WriteFrameContext(ctx context.Context, f ipc.Frame) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.WriteFrame(f)
}

// should_policy_deny_when_no_subscriber: no UI → immediate deny, no wait.
func TestShouldPolicyDenyWhenNoSubscriber(t *testing.T) {
	h := newApprovalHub()
	start := time.Now()
	allow, code := h.ask(context.Background(), testApprovalRequest("ex_1", "shutdown now"))
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
		allow, code = h.ask(context.Background(), testApprovalRequest("ex_2", "Stop-Computer"))
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
	var params struct {
		RequestSHA256 string `json:"request_sha256"`
	}
	if err := json.Unmarshal(fr.Params, &params); err != nil || params.RequestSHA256 == "" {
		t.Fatal("missing request digest", string(fr.Params))
	}
	if !h.respond(fr.ID, params.RequestSHA256, true) {
		t.Fatal("respond failed")
	}
	<-done
	if !allow || code != "" {
		t.Fatalf("allow=%v code=%s", allow, code)
	}
}

// Approval must expose the complete dangerous suffix and bind the decision to
// its canonical digest; a stale or shortened response cannot authorize it.
func TestApproval_should_bindCompleteDetailsAndRejectChangedDigest(t *testing.T) {
	h := newApprovalHub()
	s := &testSink{ch: make(chan ipc.Frame, 1)}
	h.subscribe(s)
	script := strings.Repeat("# harmless padding\n", 20) + "Restart-Computer"
	request := testApprovalRequest("ex-full", script)
	done := make(chan error, 1)
	go func() {
		allow, code := h.ask(context.Background(), request)
		if !allow || code != "" {
			done <- fmt.Errorf("allow=%v code=%s", allow, code)
			return
		}
		done <- nil
	}()
	frame := <-s.ch
	var params struct {
		Digest  string          `json:"request_sha256"`
		Details approvalDetails `json:"details"`
	}
	if err := json.Unmarshal(frame.Params, &params); err != nil {
		t.Fatal(err)
	}
	if params.Details.Script != script || !strings.Contains(params.Details.Script, "Restart-Computer") || params.Digest == "" || len(params.Details.Args) == 0 {
		t.Fatalf("complete operation was not exposed: %+v", params)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(frame.Params, &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["preview"]; ok {
		t.Fatal("preview field should not be the approval authority")
	}
	if h.respond(frame.ID, strings.Repeat("0", 64), true) {
		t.Fatal("changed digest was accepted")
	}
	if !h.respond(frame.ID, params.Digest, true) {
		t.Fatal("matching digest was rejected")
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// Program approval retains the resolved executable path and argument boundaries,
// so two executables sharing a basename cannot collapse to one preview.
func TestApproval_should_preserveResolvedProgramIdentity(t *testing.T) {
	details := detailsForExecution(execution.Request{Program: &execution.Program{Executable: "tool", Args: []string{"a b", ""}}}, exec.Command("/opt/bin/tool", "a b", ""), "/workspace", "native_user", "target")
	if details.Executable != "/opt/bin/tool" || len(details.Args) != 2 || details.Args[0] != "a b" || details.Cwd != "/workspace" {
		t.Fatalf("program identity collapsed: %+v", details)
	}
}

// Losing the final approval subscriber cancels waiting work immediately; it
// must not leave a 60-second pending approval or later report approval_timeout.
func TestApproval_should_cancelWhenLastSubscriberDisconnects(t *testing.T) {
	h := newApprovalHub()
	s := &testSink{ch: make(chan ipc.Frame, 1)}
	h.subscribe(s)
	done := make(chan string, 1)
	go func() {
		_, code := h.ask(context.Background(), testApprovalRequest("ex-disconnect", "shutdown now"))
		done <- code
	}()
	<-s.ch
	h.unsubscribe(s)
	select {
	case code := <-done:
		if code != protocol.Cancelled {
			t.Fatalf("disconnect returned %s", code)
		}
	case <-time.After(time.Second):
		t.Fatal("approval remained pending after last subscriber disconnected")
	}
	if len(h.list()) != 0 {
		t.Fatal("disconnected approval remained listed")
	}
}

type deadlineSink struct{ deadlines []time.Time }

func (s *deadlineSink) WriteFrameContext(ctx context.Context, _ ipc.Frame) error {
	deadline, ok := ctx.Deadline()
	if !ok {
		return fmt.Errorf("missing approval write deadline")
	}
	s.deadlines = append(s.deadlines, deadline)
	return nil
}

// Every approval event receives a bounded context; native IPC owns its deadline
// and serialization, rather than the hub mutating shared transport deadlines.
func TestApprovalShouldBoundEventContext(t *testing.T) {
	s := &deadlineSink{}
	if err := writeApproval(context.Background(), s, ipc.Event("a1", "approval", "")); err != nil {
		t.Fatal(err)
	}
	if len(s.deadlines) != 1 || s.deadlines[0].IsZero() || time.Until(s.deadlines[0]) > approvalWriteWait {
		t.Fatalf("unexpected deadlines: %+v", s.deadlines)
	}
}

func testApprovalRequest(execID, script string) approvalRequest {
	running := buildinfo.Running()
	return approvalRequest{ExecID: execID, Details: approvalDetails{Kind: "script", Script: script, Shell: "auto", Executable: "shell", Args: []string{"-c", script}, Cwd: "/workspace", Runtime: approvalRuntime{PID: running.PID, SHA256: running.SHA256}}}
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
