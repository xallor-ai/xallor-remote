package cli

import (
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/xallor-ai/xallor-remote/internal/execution"
)

// CLI/backend and JSON request modes must forward the exact caller selection
// to the target, never strip an unsupported name or override a request file.
func TestExecBackend_shouldForwardSelectionAndRejectMixedOverrides(t *testing.T) {
	for _, file := range []bool{false, true} {
		called := false
		cmd := executionCommand(func(_ *cobra.Command, _ string, r execution.Request) error {
			called = true
			if r.Backend != "container" {
				t.Fatal("backend selection discarded", r)
			}
			return nil
		})
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		if file {
			cmd.SetIn(strings.NewReader(`{"backend":"container","script":{"text":"echo hi"}}`))
			cmd.SetArgs([]string{"--request-file", "-"})
		} else {
			cmd.SetArgs([]string{"--backend", "container", "--", "echo hi"})
		}
		if err := cmd.Execute(); err != nil || !called {
			t.Fatal(err)
		}
	}
	cmd := executionCommand(func(*cobra.Command, string, execution.Request) error { t.Fatal("request file overridden"); return nil })
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--request-file", "-", "--backend", "native_user"})
	if cmd.Execute() == nil {
		t.Fatal("mixed request/backend modes accepted")
	}
}
