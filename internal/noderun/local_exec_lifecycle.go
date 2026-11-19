package noderun

import (
	"context"

	"github.com/xallor-ai/xallor-remote/internal/delegation"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Only device-channel notifications from Relay reach this handler. Relay
// reconstructs client cancels without a reason; unauthorized is its explicit
// authority revocation. Install the cause before waking a pending approval.
func (d *Daemon) cancelLocalExec(execID, reason string) {
	var cause error
	switch reason {
	case "":
	case protocol.Unauthorized:
		cause = delegation.ErrDenied
	default:
		return
	}
	d.mu.Lock()
	cancel := d.local[execID]
	d.mu.Unlock()
	if cancel != nil {
		cancel(cause)
	}
	d.approvals.cancelExec(execID)
}

// Register before dispatching the goroutine: cancel can be the next Relay frame,
// before payload parsing, approval, or child startup has run.
func (d *Daemon) startLocalExec(link *wsConn, msg protocol.Message) {
	ctx, cancel := context.WithCancelCause(d.ctx)
	d.mu.Lock()
	if d.ctx.Err() != nil {
		d.mu.Unlock()
		cancel(nil)
		return
	}
	d.local[msg.ExecID] = cancel
	d.localWG.Add(1)
	d.mu.Unlock()
	go func() {
		defer d.localWG.Done()
		defer func() {
			cancel(nil)
			d.mu.Lock()
			delete(d.local, msg.ExecID)
			d.mu.Unlock()
		}()
		d.runLocalExec(ctx, link, msg)
	}()
}

// Serialize shutdown with admission, then wait for real process cleanup. The
// Runtime process must not exit while foreground job owners are still running.
func (d *Daemon) waitLocalExecs() {
	d.mu.Lock()
	d.cancel()
	d.mu.Unlock()
	d.localWG.Wait()
}
