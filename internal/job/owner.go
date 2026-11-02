package job

import (
	"context"
	"errors"
	"os/exec"
	"sync"

	"github.com/xallor-ai/xallor-remote/internal/process"
)

var ErrActive = errors.New("job already has an active supervised process")

type ownedProcess interface {
	Done() <-chan struct{}
	Wait() error
}

// Owner is a local capability for one persisted occupancy. One job may run
// sequential probes and a main command; callers cannot fabricate a release ID.
type Owner struct {
	mu        sync.Mutex
	ledger    *Ledger
	id        string
	child     ownedProcess
	uncertain bool
	closed    bool
	err       error
}

func (o *Owner) ID() string { return o.id }

func (o *Owner) Start(ctx context.Context, cmd *exec.Cmd) (*process.Child, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed || o.uncertain {
		return nil, ErrUnavailable
	}
	if o.child != nil {
		select {
		case <-o.child.Done():
			if errors.Is(o.child.Wait(), process.ErrCleanup) {
				o.uncertain = true
				return nil, ErrUncertain
			}
			o.child = nil
		default:
			return nil, ErrActive
		}
	}
	if err := o.ledger.prepareStart(o.id); err != nil {
		return nil, err
	}
	child, err := process.Start(ctx, cmd)
	if err != nil {
		if cmd.Process != nil || errors.Is(err, process.ErrCleanup) {
			o.uncertain = true
		}
		return nil, err
	}
	o.child = child
	return child, nil
}

// Finish waits for the last started process boundary; earlier ones were
// confirmed settled before another Start was permitted. A nonzero root exit or
// malformed Agent result does not prevent release if cleanup is confirmed.
// Cleanup uncertainty remains occupied on disk and across Runtime restart.
func (o *Owner) Finish() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return o.err
	}
	o.closed = true
	if o.child != nil {
		if errors.Is(o.child.Wait(), process.ErrCleanup) {
			o.uncertain = true
		}
	}
	o.err = o.ledger.finish(o.id, o.uncertain)
	return o.err
}
