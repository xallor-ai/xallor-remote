package noderun

import (
	"context"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Register before dispatching the goroutine: cancel can be the next Relay frame,
// before payload parsing, approval, or child startup has run.
func (d *Daemon) startLocalExec(link *wsConn, msg protocol.Message) {
	ctx, cancel := context.WithCancel(d.ctx)
	d.mu.Lock()
	d.local[msg.ExecID] = cancel
	d.mu.Unlock()
	go func() {
		defer func() {
			cancel()
			d.mu.Lock()
			delete(d.local, msg.ExecID)
			d.mu.Unlock()
		}()
		d.runLocalExec(ctx, link, msg)
	}()
}
