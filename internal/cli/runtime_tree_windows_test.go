//go:build windows

package cli

import (
	"os/exec"
	"syscall"
)

// Hide only the fixture console; do not break away from its inherited job.
func quietTreeChild(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
