package shell

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/process"
)

// Run through the real process owner so encoded input and exit semantics are
// verified together with the native supervision boundary, not just argv shape.
func run(t *testing.T, name, script string) (string, int) {
	t.Helper()
	cmd, err := Build(name, script, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	child, err := process.Start(ctx, cmd)
	if err != nil {
		t.Fatal(err)
	}
	err = child.Wait()
	if errors.Is(err, process.ErrCleanup) {
		t.Fatal(err)
	}
	if err == nil {
		return output.String(), 0
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return output.String(), exit.ExitCode()
	}
	t.Fatal(err)
	return "", -1
}

// Each installed supported shell executes Unicode and literal metacharacters.
// Required platform shells cannot be skipped; pwsh is optional in this test.
func TestBuild_should_executeUsingSelectedInterpreter(t *testing.T) {
	names := []string{"bash", "sh"}
	if runtime.GOOS == "windows" {
		names = []string{"powershell"}
	}
	if _, _, err := resolve(runtime.GOOS, "pwsh", exec.LookPath); err == nil {
		names = append(names, "pwsh")
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			script := "printf '%s\\n' '中文 🌍 $HOME ` quoted'\nexit 7"
			if name == "powershell" || name == "pwsh" {
				script = "Write-Output '中文 🌍 $HOME ` quoted'\nexit 7"
			}
			out, code := run(t, name, script)
			if code != 7 || !strings.Contains(out, "中文 🌍 $HOME ` quoted") {
				t.Fatal(code, out)
			}
		})
	}
}

// PowerShell's native status is retained. A final cmd failure does not become
// success, and a later successful command does not inherit stale LASTEXITCODE.
func TestBuild_should_preservePowerShellStatusAndReadableErrors(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell contract")
	}
	for _, tc := range []struct {
		script string
		code   int
	}{
		{"exit 7", 7}, {"Write-Error '中文 error'", 1}, {"cmd /c exit 7", 1}, {"cmd /c exit 7; Write-Output 'done'", 0},
	} {
		out, code := run(t, "powershell", tc.script)
		if code != tc.code || strings.Contains(out, "CLIXML") {
			t.Fatal(tc, code, out)
		}
	}
}

// Invalid text and Windows expansion beyond CreateProcess limits are rejected
// before startup; Build never replaces the selected shell to make it fit.
func TestBuild_should_rejectUnrepresentableCommand(t *testing.T) {
	for _, s := range []string{"", "echo\x00x", string([]byte{0xff})} {
		if cmd, err := Build("auto", s, t.TempDir()); cmd != nil || err == nil {
			t.Fatal(cmd, err)
		}
	}
	if runtime.GOOS == "windows" {
		if cmd, err := Build("powershell", strings.Repeat("x", 16000), t.TempDir()); cmd != nil || !errors.Is(err, ErrTooLarge) {
			t.Fatal(cmd, err)
		}
	}
}
