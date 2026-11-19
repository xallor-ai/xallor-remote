//go:build windows

package cli

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"strings"
	"testing"
)

// Inspect before process creation: a background identity probe must not ask
// Windows to create a console host, even when its parent has no console.
func TestRuntimeProbeSuppressesConsoleBeforePreparingImage(t *testing.T) {
	inspected := errors.New("inspection only; no child launched")
	_, err := probeRuntimeIdentityWithImage(context.Background(), "not-executed.exe", strings.Repeat("a", 64), func(cmd *exec.Cmd, _ string) (io.Closer, error) {
		if cmd.SysProcAttr == nil || cmd.SysProcAttr.CreationFlags&0x08000000 == 0 {
			t.Fatal("background probe can create a console")
		}
		return nil, inspected
	})
	if !errors.Is(err, inspected) {
		t.Fatal(err)
	}
}
