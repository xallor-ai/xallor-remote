//go:build !windows

package runtimeowner

import "os/exec"

func hideTestProcess(cmd *exec.Cmd) {}
