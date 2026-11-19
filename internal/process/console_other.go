//go:build !windows

package process

import "os/exec"

func HideConsole(*exec.Cmd) {}
