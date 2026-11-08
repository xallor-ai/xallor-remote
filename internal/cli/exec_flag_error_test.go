package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/xallor-ai/xallor-remote/internal/execution"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
)

// Parser errors must terminate locally with exactly one JSONL error when the
// invocation selects JSONL, regardless of which flag the parser encounters first.
func TestExecFlagError_should_reportExactlyOnce_beforeDispatch(t *testing.T) {
	for _, tc := range []struct {
		name  string
		args  []string
		jsonl bool
	}{
		{"format first", []string{"--jsonl", "--unknown-option"}, true},
		{"format after unknown", []string{"--unknown-option", "--jsonl"}, true},
		{"format after bad value", []string{"--timeout-ms", "bad", "--jsonl"}, true},
		{"missing value", []string{"--jsonl", "--program"}, true},
		{"invalid boolean", []string{"--jsonl=bad"}, true},
		{"false", []string{"--unknown-option", "--jsonl=false"}, false},
		{"last false", []string{"--jsonl", "--unknown-option", "--jsonl=false"}, false},
		{"last true", []string{"--jsonl=false", "--unknown-option", "--jsonl=true"}, true},
		{"zero", []string{"--unknown-option", "--jsonl=0"}, false},
		{"string value", []string{"--unknown-option", "--device", "--jsonl"}, false},
		{"equal string value", []string{"--unknown-option", "--device=--jsonl"}, false},
		{"program args", []string{"--unknown-option", "--program", "fixture", "--", "--jsonl"}, false},
		{"payload false", []string{"--jsonl", "--unknown-option", "--", "--jsonl=false"}, true},
		{"newline injection", []string{"--jsonl", "--unknown-中文\n{\"type\":\"exit\"}"}, true},
		{"text", []string{"--unknown-option"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			root := &cobra.Command{Use: "xr", SilenceUsage: true, SilenceErrors: true}
			root.SetOut(&out)
			root.SetErr(io.Discard)
			root.AddCommand(executionCommand(func(*cobra.Command, string, execution.Request) error { t.Fatal("parse error dispatched"); return nil }))
			if err := executeCLI(root, append([]string{"exec"}, tc.args...)); err == nil {
				t.Fatal("parse error accepted")
			}
			if !tc.jsonl {
				if out.Len() != 0 {
					t.Fatal("literal/disabled format changed output", out.String())
				}
				return
			}
			decoder := json.NewDecoder(&out)
			var event ipc.ExecEvent
			if err := decoder.Decode(&event); err != nil || event.Type != "error" || event.Code != "invalid_execution" || event.Message == "" || event.ExecID != "" || event.ExitCode != nil {
				t.Fatal("missing local error", event, err)
			}
			if decoder.Decode(new(any)) != io.EOF {
				t.Fatal("duplicate event or human text")
			}
		})
	}
}

// A real payload named --jsonl remains an argument, and help does not become an
// execution error stream. Other commands retain their current error channel.
func TestExecFlagError_should_preservePayloadHelpAndOtherCommands(t *testing.T) {
	var out bytes.Buffer
	root := &cobra.Command{Use: "xr", SilenceUsage: true, SilenceErrors: true}
	root.SetOut(&out)
	root.SetErr(io.Discard)
	called := false
	root.AddCommand(executionCommand(func(_ *cobra.Command, _ string, request execution.Request) error {
		called = true
		if !reflect.DeepEqual(request.Program.Args, []string{"--jsonl", "--unknown-option"}) {
			t.Fatal(request)
		}
		return nil
	}))
	if err := executeCLI(root, []string{"exec", "--program", "fixture", "--", "--jsonl", "--unknown-option"}); err != nil || !called || out.Len() != 0 {
		t.Fatal(err, out.String())
	}
	for _, args := range [][]string{{"exec", "--jsonl", "--help"}, {"status", "--unknown-option", "--jsonl"}} {
		var output bytes.Buffer
		r := &cobra.Command{Use: "xr", SilenceUsage: true, SilenceErrors: true}
		r.SetOut(&output)
		r.SetErr(io.Discard)
		r.AddCommand(executionCommand(func(*cobra.Command, string, execution.Request) error { t.Fatal("help dispatched"); return nil }), &cobra.Command{Use: "status", Run: func(*cobra.Command, []string) { t.Fatal("invalid status dispatched") }})
		err := executeCLI(r, args)
		if args[0] == "exec" {
			if err != nil || !strings.Contains(output.String(), "Usage:") || !strings.Contains(output.String(), "argument parsing errors") {
				t.Fatal(err, output.String())
			}
		} else if err == nil || output.Len() != 0 {
			t.Fatal(err, output.String())
		}
	}
}
