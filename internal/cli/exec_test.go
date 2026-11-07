package cli

import (
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/xallor-ai/xallor-remote/internal/execution"
)

// Cobra's parsed program arguments must reach the request unchanged. Script
// fragments cannot be joined ambiguously, and invalid modes never start Runtime.
func TestExec_should_preserveProgramArgs_and_rejectAmbiguousScript(t *testing.T) {
	args := []string{"", "a b", `quote"`, "$HOME", "中文", "--name=value"}
	called := false
	c := executionCommand(func(_ *cobra.Command, device string, r execution.Request) error {
		called = true
		if device != "123456789" || r.Program.Executable != "fixture" || !reflect.DeepEqual(r.Program.Args, args) {
			t.Fatal(device, r)
		}
		return nil
	})
	c.SetArgs(append([]string{"--device", "123456789", "--program", "fixture", "--"}, args...))
	if err := c.Execute(); err != nil || !called {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"--", "echo", "a b"}, {"--program", "git", "--shell", "sh"}, {"--script-file", "x", "--program", "git"}, {"--timeout-ms", "-1", "--", "x"}} {
		c := executionCommand(func(*cobra.Command, string, execution.Request) error { t.Fatal("invalid input dispatched"); return nil })
		c.SetOut(io.Discard)
		c.SetErr(io.Discard)
		c.SetArgs(args)
		if c.Execute() == nil {
			t.Fatal(args)
		}
	}
}

// Reading local UTF-8 text avoids shell interpretation. JSON preserves empty
// arguments and rejects removed command fields before dispatch.
func TestExec_should_readUTF8Script_and_structuredRequest(t *testing.T) {
	for _, tc := range []struct {
		flag, input string
		script      bool
	}{
		{"--script-file", "\ufeff$x = '中文';\nWrite-Output $x", true},
		{"--request-file", `{"program":{"executable":"git","args":["","a b"]},"cwd":"/target"}`, false},
	} {
		called := false
		c := executionCommand(func(_ *cobra.Command, _ string, r execution.Request) error {
			called = true
			if tc.script && (r.Script == nil || r.Script.Text != strings.TrimPrefix(tc.input, "\ufeff")) {
				t.Fatal(r)
			}
			if !tc.script && (r.Program == nil || !reflect.DeepEqual(r.Program.Args, []string{"", "a b"}) || r.Cwd != "/target") {
				t.Fatal(r)
			}
			return nil
		})
		c.SetIn(strings.NewReader(tc.input))
		c.SetArgs([]string{tc.flag, "-"})
		if err := c.Execute(); err != nil || !called {
			t.Fatal(err)
		}
	}
	c := executionCommand(func(*cobra.Command, string, execution.Request) error {
		t.Fatal("obsolete request dispatched")
		return nil
	})
	c.SetOut(io.Discard)
	c.SetErr(io.Discard)
	c.SetIn(strings.NewReader(`{"command":"no"}`))
	c.SetArgs([]string{"--request-file", "-"})
	if c.Execute() == nil {
		t.Fatal("obsolete input accepted")
	}
}
