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

// Waiting for the single writer is cancellable, including management requests
// queued behind an unrelated blocked frame. Regular output retains link context.
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
	}
	defer func() { <-w.writeGate }()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if w.observe != nil {
		w.observe(m)
	}
	return w.conn.Write(ctx, websocket.MessageText, raw)
}

var errMsgTooLarge = errStr(protocol.TooLarge)
