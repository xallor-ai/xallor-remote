//go:build !windows

package runtimeprocess

import "os/exec"

func configureTestChild(*exec.Cmd) {}
