package relay

import (
	"context"
	"sync/atomic"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

type inboundFrame struct {
	message protocol.Message
	bytes   int64
}

// inbox keeps reading during bounded identity I/O so disconnect cancels database
// and admission waits. Output floods are dropped with truncation, never used to
// stop draining the device's shared socket. Non-output overload closes the socket.
type inbox struct {
	frames chan inboundFrame
	bytes  atomic.Int64
	done   chan struct{}
}

func newInbox(ctx context.Context, c *websocket.Conn, cancel context.CancelFunc, accept func(protocol.Message) bool, drop func(protocol.Message)) *inbox {
	i := &inbox{frames: make(chan inboundFrame, protocol.MaxRelayInboxFrames), done: make(chan struct{})}
	go func() {
		defer close(i.done)
		defer close(i.frames)
		defer cancel()
		for {
			readCtx, stop := context.WithTimeout(ctx, protocol.ConnStaleAfter)
			var m protocol.Message
			err := wsjson.Read(readCtx, c, &m)
			stop()
			if err != nil {
				return
			}
			if !accept(m) {
				return
			}
			if !i.offer(m, drop) {
				return
			}
		}
	}()
	return i
}

func (i *inbox) offer(m protocol.Message, drop func(protocol.Message)) bool {
	size := int64(retainedBytes(m))
	output := outputMessage(m)
	limit := int64(protocol.MaxRelayInboxBytes)
	if output {
		limit -= protocol.MaxEnvelopeBytes * protocol.MaxConcurrentExec
	}
	if i.bytes.Load()+size > limit ||
		(output && len(i.frames) >= protocol.MaxRelayInboxFrames-protocol.MaxConcurrentExec*2) {
		if output {
			drop(m)
			return true
		}
		return false
	}
	i.bytes.Add(size)
	select {
	case i.frames <- inboundFrame{message: m, bytes: size}:
		return true
	default:
		i.bytes.Add(-size)
		if output {
			drop(m)
			return true
		}
		return false
	}
}

func (h *Hub) noteDroppedOutput(from Sender, m protocol.Message) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if inf := h.inflight[m.ExecID]; inf != nil {
		if d := h.devices[inf.deviceID]; d != nil && d.conn == from {
			inf.trunc = true
		}
	}
}
