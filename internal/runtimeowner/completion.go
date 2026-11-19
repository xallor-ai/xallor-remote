package runtimeowner

import (
	"errors"

	"github.com/xallor-ai/xallor-remote/internal/release"
)

// Complete considers both the full backend result and durable effects. An empty
// effect list cannot erase cleanup uncertainty from a source/probe preflight.
func (t *Transaction) Complete(cause error) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.running {
		return ErrRecorded
	}
	return t.completeLocked(cause)
}

func (t *Transaction) completeRun(cause error) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.running {
		return ErrRecorded
	}
	return t.completeLocked(cause)
}

func (t *Transaction) completeLocked(cause error) error {
	uncertain := func(err error) error { return errors.Join(release.ErrCleanupUncertain, ErrUncertain, cause, err) }
	if t.closed || t.poisoned || t.record.Outcome == Uncertain {
		return uncertain(nil)
	}
	if t.record.Outcome != Pending {
		return errors.Join(cause, ErrRecorded)
	}
	if t.hasUnsettledResourceLocked() {
		cause = errors.Join(cause, release.ErrCleanupUncertain)
	}
	next := t.record
	switch {
	case cause == nil:
		next.Outcome = Succeeded
	case errors.Is(cause, release.ErrCleanupUncertain) || errors.Is(cause, ErrUncertain) || t.record.unresolved():
		next.Outcome = Uncertain
	case len(t.record.Effects) == 0:
		next.Outcome = Rejected
	default:
		next.Outcome = Failed
	}
	if !next.valid() {
		next.Outcome = Uncertain
	}
	if err := t.save(next); err != nil {
		return uncertain(err)
	}
	if next.Outcome == Uncertain {
		return uncertain(nil)
	}
	return cause
}
