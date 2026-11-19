//go:build windows

package nativepipe

import (
	"errors"
	"io"

	"golang.org/x/sys/windows"
)

func Prepare(fd uintptr) error {
	mode := uint32(windows.PIPE_READMODE_BYTE | windows.PIPE_NOWAIT)
	if err := windows.SetNamedPipeHandleState(windows.Handle(fd), &mode, nil, nil); err != nil {
		return err
	}
	var observed uint32
	if err := windows.GetNamedPipeHandleState(windows.Handle(fd), &observed, nil, nil, nil, nil, 0); err != nil {
		return err
	}
	if observed&windows.PIPE_NOWAIT == 0 {
		return errors.New("pipe did not enter nonblocking mode")
	}
	return nil
}

func ReadAvailable(fd uintptr, buffer []byte) (int, error) {
	var n uint32
	err := windows.ReadFile(windows.Handle(fd), buffer, &n, nil)
	if errors.Is(err, windows.ERROR_NO_DATA) {
		return int(n), nil
	}
	if errors.Is(err, windows.ERROR_BROKEN_PIPE) {
		return int(n), io.EOF
	}
	return int(n), err
}
