package ipc_test

import (
	"bytes"
	"errors"
	"io"
	"net"
	"strings"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/ipc"
)

// Exit success requires a matching, valid completed terminal with an explicit zero exit code.
func TestConsumeExec_should_rejectUnconfirmedOrFailedResult(t *testing.T) {
	for _, tc := range []struct {
		name    string
		frame   ipc.Frame
		success bool
	}{
		{"zero", ipc.OK("e1", map[string]any{"status": "completed", "exit_code": 0}), true},
		{"nonzero", ipc.OK("e1", map[string]any{"status": "completed", "exit_code": 7}), false},
		{"missing_exit", ipc.OK("e1", map[string]any{"status": "completed"}), false},
		{"missing_status", ipc.OK("e1", map[string]any{"exit_code": 0}), false},
		{"null_exit", ipc.OK("e1", map[string]any{"status": "completed", "exit_code": nil}), false},
		{"string_exit", ipc.OK("e1", map[string]any{"status": "completed", "exit_code": "0"}), false},
		{"fraction_exit", ipc.OK("e1", map[string]any{"status": "completed", "exit_code": 0.5}), false},
		{"unknown_event", ipc.Frame{ID: "e1", Event: "unknown"}, false},
		{"mixed_event", ipc.Frame{ID: "e1", Event: "stdout", OK: boolPtr(true)}, false},
		{"mixed_terminal", ipc.Frame{ID: "e1", OK: boolPtr(true), Code: "agent_error", Result: []byte(`{"status":"completed","exit_code":0}`)}, false},
		{"wrong_id", ipc.OK("other", map[string]any{"status": "completed", "exit_code": 0}), false},
		{"cancelled", ipc.OK("e1", map[string]any{"status": "cancelled", "exit_code": 0}), false},
		{"timeout", ipc.OK("e1", map[string]any{"status": "timeout", "exit_code": 0}), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			read := func() (ipc.Frame, error) { return tc.frame, nil }
			if err := ipc.ConsumeExec("e1", read, io.Discard, io.Discard); (err == nil) != tc.success {
				t.Fatalf("err=%v", err)
			}
		})
	}
	if ipc.ConsumeExec("e1", func() (ipc.Frame, error) { return ipc.Frame{}, io.EOF }, io.Discard, io.Discard) == nil {
		t.Fatal("EOF without terminal reported success")
	}
}

func boolPtr(b bool) *bool { return &b }

// A real framed connection delivers output then closes. The decoder/consumer
// must preserve output while reporting uncertainty instead of terminal success.
func TestConsumeExec_should_preserveOutput_when_connectionClosesWithoutTerminal(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	go func() {
		defer b.Close()
		_ = ipc.NewConn(b).WriteFrame(ipc.Event("e1", "stdout", "observed"))
	}()
	var out bytes.Buffer
	err := ipc.ConsumeExec("e1", ipc.NewConn(a).ReadFrame, &out, io.Discard)
	if !errors.Is(err, io.EOF) || out.String() != "observed" {
		t.Fatal(err, out.String())
	}
}

// A failed process can still have truncated output; both facts must be exposed.
func TestConsumeExec_should_reportTruncation_when_processFails(t *testing.T) {
	var diagnostic bytes.Buffer
	read := func() (ipc.Frame, error) {
		return ipc.OK("e1", map[string]any{"status": "completed", "exit_code": 7, "truncated": true}), nil
	}
	err := ipc.ConsumeExec("e1", read, io.Discard, &diagnostic)
	if err == nil || !strings.Contains(err.Error(), "7") || !strings.Contains(diagnostic.String(), "截断") {
		t.Fatal(err, diagnostic.String())
	}
}
