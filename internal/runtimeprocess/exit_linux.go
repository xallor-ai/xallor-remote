//go:build linux

package runtimeprocess

import (
	"context"
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

type processWatch struct {
	fd     int
	closed bool
}

func watchExit(pid int) (ExitWatch, error) {
	fd, err := unix.PidfdOpen(pid, 0)
	if errors.Is(err, unix.ESRCH) {
		return nil, ErrProcessGone
	}
	if err != nil {
		return nil, err
	}
	return &processWatch{fd: fd}, nil
}

func (w *processWatch) Wait(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		exited, err := w.poll(100)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil || exited {
			return err
		}
	}
}

func (w *processWatch) Exited() (bool, error) {
	for {
		exited, err := w.poll(0)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		return exited, err
	}
}

func (w *processWatch) poll(milliseconds int) (bool, error) {
	if w.closed {
		return false, os.ErrClosed
	}
	fds := []unix.PollFd{{Fd: int32(w.fd), Events: unix.POLLIN}}
	_, err := unix.Poll(fds, milliseconds)
	if err != nil {
		return false, err
	}
	if fds[0].Revents&(unix.POLLIN|unix.POLLHUP) != 0 {
		return true, nil
	}
	if fds[0].Revents != 0 {
		return false, fmt.Errorf("unexpected Runtime pidfd wait result: %d", fds[0].Revents)
	}
	return false, nil
}

func (w *processWatch) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	return unix.Close(w.fd)
}
