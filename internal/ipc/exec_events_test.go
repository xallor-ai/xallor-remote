package ipc_test

import (
	"errors"
	"io"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/ipc"
)

// The machine stream must have one terminal, retain observed identity and never
// invent an exit after EOF, malformed fields or mismatched execution identity.
func TestReadExec_should_emitOneTerminal_and_preserveCertainty(t *testing.T) {
	for _, tc := range []struct {
		name       string
		terminal   ipc.Frame
		kind, code string
		success    bool
	}{
		{"success", ipc.OK("e1", map[string]any{"status": "completed", "exit_code": 0, "duration_ms": 0, "truncated": false, "exec_id": "job"}), "exit", "", true},
		{"failure", ipc.OK("e1", map[string]any{"status": "completed", "exit_code": 7}), "exit", "", false},
		{"cancelled", ipc.OK("e1", map[string]any{"status": "cancelled", "exit_code": 1}), "exit", "", false},
		{"timeout", ipc.OK("e1", map[string]any{"status": "timeout", "exit_code": 1}), "exit", "", false},
		{"null", ipc.OK("e1", map[string]any{"status": "completed", "exit_code": nil}), "error", "result_unknown", false},
		{"other job", ipc.OK("e1", map[string]any{"status": "completed", "exit_code": 0, "exec_id": "other"}), "error", "result_unknown", false},
		{"negative duration", ipc.OK("e1", map[string]any{"status": "completed", "exit_code": 0, "duration_ms": -1}), "error", "result_unknown", false},
		{"remote rejection", ipc.Fail("e1", "program_unavailable", "unused"), "error", "program_unavailable", false},
		{"missing error code", ipc.Fail("e1", "", "unused"), "error", "result_unknown", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frames := []ipc.Frame{{ID: "e1", Event: "stdout", Data: "first\nsecond", ExecID: "job"}, tc.terminal}
			var events []ipc.ExecEvent
			read := func() (ipc.Frame, error) {
				if len(frames) == 0 {
					t.Fatal("read past terminal")
				}
				f := frames[0]
				frames = frames[1:]
				return f, nil
			}
			err := ipc.ReadExec("e1", read, func(e ipc.ExecEvent) error { events = append(events, e); return nil })
			if (err == nil) != tc.success || len(events) != 2 || events[0].Data != "first\nsecond" || events[1].Type != tc.kind || events[1].Code != tc.code || events[1].ExecID != "job" {
				t.Fatal(err, events)
			}
			if tc.kind == "error" && events[1].ExitCode != nil {
				t.Fatal("fabricated exit", events[1])
			}
		})
	}
	var events []ipc.ExecEvent
	err := ipc.ReadExec("e1", func() (ipc.Frame, error) { return ipc.Frame{}, io.EOF }, func(e ipc.ExecEvent) error { events = append(events, e); return nil })
	if !errors.Is(err, io.EOF) || len(events) != 1 || events[0].Type != "error" || events[0].Code != "result_unknown" {
		t.Fatal(err, events)
	}
}

// A broken output consumer must stop reading immediately rather than swallowing
// the error or appending another terminal to a partially written JSON line.
func TestReadExec_should_stop_when_eventSinkFails(t *testing.T) {
	want := errors.New("broken sink")
	reads, writes := 0, 0
	err := ipc.ReadExec("e1", func() (ipc.Frame, error) { reads++; return ipc.Event("e1", "stdout", "data"), nil }, func(ipc.ExecEvent) error { writes++; return want })
	if !errors.Is(err, want) || reads != 1 || writes != 1 {
		t.Fatal(err, reads, writes)
	}
}
