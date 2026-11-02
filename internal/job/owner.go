package job

import (
	"context"
	"errors"
	"os/exec"
	"sync"

	"github.com/xallor-ai/xallor-remote/internal/process"
)

// Owner is a local capability for one persisted occupancy. One job may run
// sequential probes and a main command; callers cannot fabricate a release ID.
type Owner struct {
	mu        sync.Mutex
	ledger    *Ledger
	id        string
	children  []*process.Child
	uncertain bool
	closed    bool
	err       error
}

func (o *Owner) ID() string { return o.id }

func (o *Owner) Start(ctx context.Context, cmd *exec.Cmd) (*process.Child, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed || o.uncertain || !o.ledger.mayStart(o.id) {
		return nil, ErrUnavailable
	}
	child, err := process.Start(ctx, cmd)
	if err != nil {
		if cmd.Process != nil || errors.Is(err, process.ErrCleanup) {
			o.uncertain = true
		}
		return nil, err
	}
	o.children = append(o.children, child)
	return child, nil
}

// Finish waits for every started process boundary. A nonzero root exit or
// malformed Agent result does not prevent release if cleanup is confirmed.
// Cleanup uncertainty remains occupied on disk and across Runtime restart.
func (o *Owner) Finish() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return o.err
	}
	o.closed = true
	for _, child := range o.children {
		if errors.Is(child.Wait(), process.ErrCleanup) {
			o.uncertain = true
		}
	}
	o.err = o.ledger.finish(o.id, o.uncertain)
	return o.err
}
