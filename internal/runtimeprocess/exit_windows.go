//go:build windows

package runtimeprocess

import (
	"context"
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

type processWatch struct{ handle windows.Handle }

func watchExit(pid int) (ExitWatch, error) {
	if uint64(pid) > uint64(^uint32(0)) {
		return nil, errors.New("invalid Runtime process id")
	}
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return nil, ErrProcessGone
	}
	if err != nil {
		return nil, err
	}
	return &processWatch{handle: h}, nil
}

func (w *processWatch) Wait(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		exited, err := w.poll(100)
		if err != nil || exited {
			return err
		}
	}
}

func (w *processWatch) Exited() (bool, error) { return w.poll(0) }

func (w *processWatch) poll(milliseconds uint32) (bool, error) {
	if w.handle == 0 {
		return false, os.ErrClosed
	}
	state, err := windows.WaitForSingleObject(w.handle, milliseconds)
	if err != nil {
		return false, err
	}
	switch state {
	case windows.WAIT_OBJECT_0:
		return true, nil
	case uint32(windows.WAIT_TIMEOUT):
		return false, nil
	default:
		return false, fmt.Errorf("unexpected Runtime process wait result: %d", state)
	}
}

func (w *processWatch) Close() error {
	if w.handle == 0 {
		return nil
	}
	h := w.handle
	w.handle = 0
	return windows.CloseHandle(h)
}
