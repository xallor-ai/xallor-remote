//go:build windows

package filelock

import (
	"fmt"
	"golang.org/x/sys/windows"
)

// Sharing is disabled so competing processes cannot acquire this lock.
func Acquire(path string) (func(), error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_ALWAYS, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, fmt.Errorf("operation already running: %w", err)
	}
	return func() { _ = windows.CloseHandle(h) }, nil
}
