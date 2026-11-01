package cli

import (
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"io"
	"testing"
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
		{"wrong_id", ipc.OK("other", map[string]any{"status": "completed", "exit_code": 0}), false},
		{"cancelled", ipc.OK("e1", map[string]any{"status": "cancelled", "exit_code": 0}), false},
		{"timeout", ipc.OK("e1", map[string]any{"status": "timeout", "exit_code": 0}), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			read := func() (ipc.Frame, error) { return tc.frame, nil }
			if err := consumeExec(read, io.Discard, io.Discard); (err == nil) != tc.success {
				t.Fatalf("err=%v", err)
			}
		})
	}
	if consumeExec(func() (ipc.Frame, error) { return ipc.Frame{}, io.EOF }, io.Discard, io.Discard) == nil {
		t.Fatal("EOF without terminal reported success")
	}
}
