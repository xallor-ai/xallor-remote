//go:build windows

package noderun

import (
	"os/exec"
	"syscall"
)

func quietOwnerVerifier(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
