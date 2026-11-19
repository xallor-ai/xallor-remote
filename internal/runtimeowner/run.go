package runtimeowner

import (
	"errors"

	"github.com/xallor-ai/xallor-remote/internal/release"
)

// Run uses the exact admitted intent once, then derives its receipt from the
// whole release result. Its lifetime is the worker's, never a head connection's.
func (t *Transaction) Run(command Command, store release.Store, control release.Control) error {
	return t.runWithSettlement(command, store, control, nil)
}

// runWithSettlement keeps completion private and owned by this invocation. A
// duplicate invocation cannot execute settlement or complete the active run.
func (t *Transaction) runWithSettlement(command Command, store release.Store, control release.Control, settle func() error) error {
	request, err := command.Request()
	t.mu.Lock()
	if t.closed || t.poisoned || t.running || t.record.Outcome != Pending {
		t.mu.Unlock()
		return ErrRecorded
	}
	t.running = true
	matches := t.record.Request == request
	t.mu.Unlock()
	complete := func(cause error) error {
		if settle != nil {
			cause = errors.Join(cause, settle())
		}
		return t.completeRun(cause)
	}
	if err != nil {
		return complete(err)
	}
	if !matches {
		return complete(ErrConflict)
	}
	tracked := store.WithEffects(t).WithResourceRecorder(releaseResources{transaction: t})
	switch command.Kind {
	case "install":
		err = tracked.Install(command.Bundle, control)
	case "rollback":
		err = tracked.Rollback(control)
	default:
		err = ErrInvalid
	}
	return complete(err)
}

// RefuseBootstrap distinguishes absent history from unfinished or unreadable
// history. It never modifies the journal or attempts to recover an old request.
func (j Journal) RefuseBootstrap() error {
	_, err := j.Active()
	if errors.Is(err, ErrUnknown) {
		return j.refuseBootstrapResources()
	}
	if err != nil {
		return err
	}
	return ErrPending
}
