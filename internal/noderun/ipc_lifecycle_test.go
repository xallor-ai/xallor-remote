package noderun

import (
	"context"
	"encoding/json"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/process"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
	"github.com/xallor-ai/xallor-remote/internal/shell"
	"net"
	"testing"
	"time"
)

// A slow peer dial may finish after its GUI window closes. Never send that invoke.
func TestForward_should_notDispatch_afterHeadClosed(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	c := ipc.NewConn(a)
	_ = c.Close()
	d := &Daemon{remote: map[string]*remoteExec{}}
	// A nil link intentionally proves no network send/subscription is attempted.
	d.forwardInvoke(c, "request", "123456789", nil, nil, protocol.OpExec, "exec", nil, true)
	if len(d.remote) != 0 {
		t.Fatal("closed head acquired a task")
	}
}

// Cancel after constructing the child but before Start: classify the proven cancellation.
func TestStart_should_reportCancellation_whenCancelWinsBeforeStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cmd, err := shell.Build("auto", "echo must-not-run", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if _, err := process.Start(ctx, cmd); err == nil {
		t.Fatal("cancelled command started")
	}
	if code := startFailureCode(ctx); code != protocol.Cancelled {
		t.Fatal(code)
	}
}

// Missing exit_code must cross IPC as unknown/null, never fabricated success.
func TestConsume_should_preserveMissingExitCode(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	_ = b.SetDeadline(time.Now().Add(time.Second))
	w := newExecWaiter()
	w.push(protocol.Message{Type: protocol.TypeExit, ExecID: "exec", Status: protocol.ExitCompleted})
	d := &Daemon{}
	go d.consumeClient(ipc.NewConn(a), "req", "exec", "123456789", nil, w, true)
	frame, err := ipc.NewConn(b).ReadFrame()
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if json.Unmarshal(frame.Result, &result) != nil {
		t.Fatal("bad result")
	}
	if result["exit_code"] != nil {
		t.Fatalf("fabricated exit code: %v", result)
	}
}
