//go:build !windows

package noderun

import "os/exec"

func quietOwnerVerifier(*exec.Cmd) {}
