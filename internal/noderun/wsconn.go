package noderun

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/coder/websocket"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

type wsConn struct {
	gateOnce  sync.Once
	writeGate chan struct{}
	ctx       context.Context
	conn      *websocket.Conn
	relay     string
	observe   func(protocol.Message)
}

func (w *wsConn) send(m protocol.Message) error { return w.sendContext(w.ctx, m) }

// Request cancellation stops admission or waiting, never an admitted socket
// write: coder/websocket closes the connection when a write context expires.
// One bounded, connection-owned writer retains the gate until its frame ends.
func (w *wsConn) sendContext(ctx context.Context, m protocol.Message) error {
	raw, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if len(raw) > protocol.MaxWSMessageBytes {
		return errMsgTooLarge
	}
	w.gateOnce.Do(func() { w.writeGate = make(chan struct{}, 1) })
	select {
	case w.writeGate <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	case <-w.ctx.Done():
		return w.ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		<-w.writeGate
		return err
	}
	if err := w.ctx.Err(); err != nil {
		<-w.writeGate
		return err
	}
	writeCtx, stop := context.WithTimeout(w.ctx, protocol.RelayWriteTimeout)
	done := make(chan error, 1)
	go func() {
		defer stop()
		defer func() { <-w.writeGate }()
		if w.observe != nil {
			w.observe(m)
		}
		done <- w.conn.Write(writeCtx, websocket.MessageText, raw)
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-w.ctx.Done():
		return w.ctx.Err()
	}
}

var errMsgTooLarge = errStr(protocol.TooLarge)
