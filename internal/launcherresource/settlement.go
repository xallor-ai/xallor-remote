// Package launcherresource settles resources owned by a native launcher.
package launcherresource

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.com/xallor-ai/xallor-remote/internal/release"
)

// Owned caches the first close result, including a failed close. Retrying a
// native handle close cannot prove that the original resource was released.
type Owned struct {
	closer io.Closer
	once   sync.Once
	result error
}

func Own(c io.Closer) *Owned { return &Owned{closer: c} }
func (o *Owned) Close() error {
	o.once.Do(func() {
		if err := o.closer.Close(); err != nil {
			o.result = errors.Join(release.ErrCleanupUncertain, err)
		}
	})
	return o.result
}
func CloseAll(resources ...*Owned) error {
	var result error
	for _, resource := range resources {
		result = errors.Join(result, resource.Close())
	}
	return result
}

type Attempt struct {
	done   chan struct{}
	result error
}

func (a *Attempt) Done() <-chan struct{} { return a.done }
func (a *Attempt) Result() error         { <-a.done; return a.result }

// Start owns the already-started process wait and observational reader. Context
// cancellation only stops observation; the attempt retains the process until
// actual exit. Delivery failure is separate from resource settlement.
func Start(ctx context.Context, wait func() error, observe func(context.Context) error, resources ...*Owned) (*Attempt, <-chan error) {
	a := &Attempt{done: make(chan struct{})}
	readCtx, cancel := context.WithCancel(ctx)
	acknowledged := make(chan error, 1)
	readDone := make(chan struct{})
	go func() { defer close(readDone); defer cancel(); acknowledged <- observe(readCtx) }()
	go func() {
		waitErr := wait()
		if waitErr != nil {
			waitErr = errors.Join(release.ErrCleanupUncertain, waitErr)
		}
		cancel()
		<-readDone
		a.result = errors.Join(waitErr, CloseAll(resources...))
		close(a.done)
	}()
	return a, acknowledged
}
