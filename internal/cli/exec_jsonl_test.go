package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"testing"

	"github.com/spf13/cobra"
	"github.com/xallor-ai/xallor-remote/internal/execution"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
)

// Invalid semantic inputs produce a single machine-readable error before any
// Runtime dispatch. JSONL selection cannot change the validated execution input.
func TestExecJSONL_should_reportInvalidInput_before_dispatch(t *testing.T) {
	var out bytes.Buffer
	c := executionCommand(func(*cobra.Command, string, execution.Request) error {
		t.Fatal("dispatched invalid request")
		return nil
	})
	// Match the real root's error/usage policy; Cobra otherwise appends help when
	// this subcommand is executed as a standalone test root.
	c.SilenceUsage, c.SilenceErrors = true, true
	c.SetOut(&out)
	c.SetErr(io.Discard)
	c.SetArgs([]string{"--jsonl", "--", "echo", "fragment"})
	if c.Execute() == nil {
		t.Fatal("invalid input accepted")
	}
	d := json.NewDecoder(&out)
	var event ipc.ExecEvent
	if err := d.Decode(&event); err != nil || event.Type != "error" || event.Code != "invalid_execution" || event.ExitCode != nil {
		t.Fatal(err, event)
	}
	if d.Decode(new(any)) != io.EOF {
		t.Fatal("extra output")
	}
}
