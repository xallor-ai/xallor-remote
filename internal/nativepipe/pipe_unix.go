//go:build !windows

package nativepipe

import (
	"errors"
	"io"

	"golang.org/x/sys/unix"
)

func Prepare(fd uintptr) error { return unix.SetNonblock(int(fd), true) }
func ReadAvailable(fd uintptr, buffer []byte) (int, error) {
	n, err := unix.Read(int(fd), buffer)
	if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EINTR) {
		return 0, nil
	}
	if err == nil && n == 0 {
		return 0, io.EOF
	}
	// Unix syscalls report -1 on failure; io.Reader consumers require a
	// nonnegative count even when preserving that native error.
	if n < 0 {
		n = 0
	}
	return n, err
}
