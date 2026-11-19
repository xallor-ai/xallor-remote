//go:build windows

package runtimeprocess

import (
	"os/exec"
	"syscall"
)

func configureTestChild(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
