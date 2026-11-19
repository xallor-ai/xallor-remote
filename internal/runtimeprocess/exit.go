// Package runtimeprocess observes Runtime process lifetime independently of IPC.
// An observer holds a kernel identity and never sends a process signal.
package runtimeprocess

import (
	"context"
	"errors"
)

var ErrProcessGone = errors.New("process does not exist")
var ErrUnsupported = errors.New("process exit observation is unsupported")

type ExitWatch interface {
	Exited() (bool, error)
	Wait(context.Context) error
	Close() error
}

// WatchExit returns a handle to this process incarnation. Wait and Close are
// called serially by the owner. A missing endpoint is never used as exit proof.
func WatchExit(pid int) (ExitWatch, error) {
	if pid <= 0 {
		return nil, errors.New("invalid Runtime process id")
	}
	return watchExit(pid)
}
