package cli

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// launchCommitGate owns the observer after the admission waiter is joined.
// Daemon shutdown need not join every IPC goroutine to close the handle safely.
type launchCommitGate struct {
	mu            sync.Mutex
	closed        bool
	selected      func() (bool, error)
	exited        func() (bool, error)
	closeObserver func() error
	deadline      time.Time
}

func (g *launchCommitGate) Check() (bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return false, errors.New("Runtime launch observer is closed")
	}
	return checkLaunchCommit(g.selected, g.exited, g.deadline)
}

func (g *launchCommitGate) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return nil
	}
	g.closed = true
	return g.closeObserver()
}

// A selected owner is independent of its head. Until that commit, retain the
// kernel parent observer and bound waiting so an abandoned transition cannot
// leave an indefinitely running diagnostics-only service. The state is read
// again on parent loss/expiry to allow a commit that raced the first read.
func checkLaunchCommit(selected func() (bool, error), parentExited func() (bool, error), deadline time.Time) (bool, error) {
	ready, err := selected()
	if err != nil || ready {
		return ready, err
	}
	gone, watchErr := parentExited()
	if !gone && watchErr == nil && time.Now().Before(deadline) {
		return false, nil
	}
	ready, err = selected()
	if err != nil || ready {
		return ready, err
	}
	if watchErr != nil {
		return false, fmt.Errorf("Runtime launch parent observation failed: %w", watchErr)
	}
	if gone {
		return false, errors.New("Runtime launch parent exited before selection commit")
	}
	return false, errors.New("Runtime launch selection commit timed out")
}
