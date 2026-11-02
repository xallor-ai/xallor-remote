package tui

import (
	"os"
	"runtime"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Losing the local execution channel cannot make the UI show completion.
func TestWaitExec_should_reportUnknown_when_channelMissingOrClosed(t *testing.T) {
	previous := execCh
	defer func() { execCh = previous }()
	for _, closed := range []bool{false, true} {
		execCh = nil
		if closed {
			execCh = make(chan tea.Msg)
			close(execCh)
		}
		got, ok := waitExec()().(execDone)
		if !ok || got.err == nil {
			t.Fatal("missing terminal treated as success", got)
		}
		updated, _ := (model{busy: true}).Update(got)
		if !strings.Contains(updated.View(), "执行结果未确认") {
			t.Fatal(updated.View())
		}
	}
}

// The owned E2E harness supplies a real controller profile and one peer. This
// exercises the exact TUI RPC path through Runtime and Relay to an OS process.
func TestStreamExecE2E_should_showFailure_and_keepOutput(t *testing.T) {
	if os.Getenv("XR_TUI_RESULT_E2E") != "1" {
		t.Skip("requires owned Runtime cluster")
	}
	for _, tc := range []struct {
		command string
		success bool
	}{{"echo tui-observed", true}, {"echo tui-observed; exit 7", false}} {
		var out strings.Builder
		err := streamExec(tc.command, func(s string) { out.WriteString(s) })
		if (err == nil) != tc.success || !strings.Contains(out.String(), "tui-observed") {
			t.Fatal(tc.command, err, out.String())
		}
		if err != nil && !strings.Contains(err.Error(), "7") {
			t.Fatal(err)
		}
		updated, _ := (model{busy: true}).Update(execDone{err: err})
		if err != nil && !strings.Contains(updated.View(), "退出码 7") {
			t.Fatal(updated.View())
		}
	}
	// An unsupported language is not used: each target gets its own valid shell.
	command := "printf tui-stderr >&2; exit 3"
	if runtime.GOOS == "windows" {
		command = "[Console]::Error.WriteLine('tui-stderr'); exit 3"
	}
	var out strings.Builder
	err := streamExec(command, func(s string) { out.WriteString(s) })
	if err == nil || !strings.Contains(err.Error(), "3") || !strings.Contains(out.String(), "tui-stderr") {
		t.Fatal(err, out.String())
	}
}
