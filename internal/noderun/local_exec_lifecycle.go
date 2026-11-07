package noderun

import (
	"context"

	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

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
