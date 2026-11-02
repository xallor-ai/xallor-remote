// Package process owns command startup, cancellation and descendant reclamation.
// It is process supervision, not an OS security sandbox.
package process

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"time"
)

// ErrCleanup means the supervised process group could not be confirmed stopped.
// Callers must not turn this into a successful terminal or proof of resource release.
var ErrCleanup = errors.New("process cleanup unconfirmed")

const cleanupTimeout = 5 * time.Second

type boundary interface {
	start(*exec.Cmd) error
	wait(*exec.Cmd) error
	attach(*exec.Cmd) error
	waitRoot() error
	stop() error
	settle() error
	close() error
}

type Child struct {
	mu      sync.Mutex
	group   boundary
	closed  bool
	stopErr error
	done    chan struct{}
	err     error
}

// Start takes sole ownership of an unstarted exec.Command. On Windows, commands
// are created suspended with PROC_THREAD_ATTRIBUTE_JOB_LIST,
// so the OS assigns the child before any user code runs. CommandContext is
// rejected: cancellation must target the group, not race a second root killer.
// Reaping starts immediately. Streaming readers must use their own os.Pipe
// files, not Cmd.StdoutPipe/StderrPipe, which Cmd.Wait closes automatically.
func Start(ctx context.Context, cmd *exec.Cmd) (*Child, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if cmd.Cancel != nil || cmd.Process != nil {
		return nil, errors.New("command already owns a process or cancellation")
	}
	g, err := prepare(cmd)
	if err != nil {
		return nil, err
	}
	return startPrepared(ctx, cmd, g)
}

func startPrepared(ctx context.Context, cmd *exec.Cmd, g boundary) (*Child, error) {
	if err := ctx.Err(); err != nil {
		_ = g.close()
		return nil, err
	}
	cmd.WaitDelay = time.Second
	if err := g.start(cmd); err != nil {
		_ = g.close()
		return nil, err
	}
	if err := g.attach(cmd); err != nil {
		// The Windows root is still suspended until attach succeeds. Never fall
		// back to an unsupervised process when job assignment/resume fails.
		_ = g.stop()
		_ = cmd.Process.Kill()
		_ = g.wait(cmd)
		_ = g.close()
		return nil, fmt.Errorf("process supervision: %w", err)
	}
	c := &Child{group: g, done: make(chan struct{})}
	go func() {
		select {
		case <-ctx.Done():
			_ = c.Stop()
		case <-c.done:
		}
	}()
	go func() {
		// Linux observes exit without reaping, keeping the group ID reserved
		// until descendant cleanup. Windows retains a native process handle.
		rootErr := g.waitRoot()
		c.mu.Lock()
		cleanupErr := errors.Join(rootErr, g.stop(), g.settle(), c.stopErr)
		closeErr := g.close()
		c.closed = true
		c.mu.Unlock()
		waitErr := g.wait(cmd)
		if cleanupErr = errors.Join(cleanupErr, closeErr); cleanupErr != nil {
			cleanupErr = fmt.Errorf("%w: %v", ErrCleanup, cleanupErr)
		}
		c.err = errors.Join(waitErr, cleanupErr)
		close(c.done)
	}()
	return c, nil
}

// Stop is idempotent and cannot signal a recycled process/group after Wait.
func (c *Child) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	err := c.group.stop()
	c.stopErr = errors.Join(c.stopErr, err)
	return err
}

// Wait can be called repeatedly or concurrently. Success includes group cleanup.
func (c *Child) Wait() error { <-c.done; return c.err }
