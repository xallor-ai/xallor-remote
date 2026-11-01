//go:build windows

package noderun

import (
	"os/exec"
	"strings"
	"testing"
)

func psExit(t *testing.T, command string) int {
	t.Helper()
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", command)
	err := cmd.Run()
	if err == nil {
		return 0
	}
	ee, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("run %q: %v", command, err)
	}
	return ee.ExitCode()
}

func TestWrapCommand_should_set_encoding_without_lastexitcode(t *testing.T) {
	got := wrapCommand("cmd /c exit 7")
	if !strings.Contains(got, "OutputEncoding") {
		t.Fatalf("missing encoding prefix: %s", got)
	}
	if strings.Contains(got, "LASTEXITCODE") {
		t.Fatal("wrapper must not promote LASTEXITCODE")
	}
}

func TestWrapCommand_should_keep_native_powershell_exit_code(t *testing.T) {
	if code := psExit(t, wrapCommand("exit 7")); code != 7 {
		t.Fatalf("exit 7 → %d", code)
	}
}

func TestWrapCommand_should_keep_write_error_as_one(t *testing.T) {
	if code := psExit(t, wrapCommand(`Write-Error "x"`)); code != 1 {
		t.Fatalf("Write-Error → %d", code)
	}
	if code := psExit(t, wrapCommand(`cmd; Write-Error "x"`)); code != 1 {
		t.Fatalf("cmd; Write-Error → %d", code)
	}
}

func TestWrapCommand_should_match_unwrapped_when_cmd_exits_seven(t *testing.T) {
	wrapped := psExit(t, wrapCommand("cmd /c exit 7"))
	plain := psExit(t, "cmd /c exit 7")
	if wrapped != plain {
		t.Fatalf("wrapped=%d plain=%d", wrapped, plain)
	}
}
