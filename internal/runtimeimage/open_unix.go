//go:build !windows && !linux

package runtimeimage

import (
	"os"
	"os/exec"

	"golang.org/x/sys/unix"
)

// A nonblocking descriptor rejects FIFO/device inputs without waiting for a
// writer. This is digest admission, not an immutable POSIX execution snapshot.
func openImage(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW, 0)
}

// POSIX pathname execution is still unsealed. Retaining a read descriptor is
// insufficient to prevent replacement; do not present this as protection.
func inheritImage(_ *exec.Cmd, _ *os.File) error { return nil }
