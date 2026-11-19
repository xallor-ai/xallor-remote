//go:build !windows

package cli

import "os/exec"

// Preserve the supervised parent's process group.
func quietTreeChild(*exec.Cmd) {}
