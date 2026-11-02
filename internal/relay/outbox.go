package relay

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

var (
	errOutputDropped = errors.New("output buffer full")
	errOutboxClosed  = errors.New("connection writer closed")
)

type queuedMessage struct {
	message protocol.Message
	bytes   int
}

// outbox owns the only network writer. Enqueue and Close never perform I/O.
// The Hub can serialize queue admission with routing state without waiting on peers.
type outbox struct {
	mu         sync.Mutex
	queue      []queuedMessage
	bytes      int
	buffered   map[string]int
	closing    bool
	timer      *time.Timer
	wake       chan struct{}
	done       chan struct{}
	ctx        context.Context
	cancel     context.CancelFunc
	write      func(context.Context, protocol.Message) error
	disconnect func()
}

func newOutbox(parent context.Context, write func(context.Context, protocol.Message) error, disconnect func()) *outbox {
	ctx, cancel := context.WithCancel(parent)
	b := &outbox{buffered: make(map[string]int), wake: make(chan struct{}, 1), done: make(chan struct{}), ctx: ctx, cancel: cancel, write: write, disconnect: disconnect}
	go b.run()
	return b
}

func outputMessage(m protocol.Message) bool {
	return m.Type == protocol.TypeStdout || m.Type == protocol.TypeStderr
}

func (b *outbox) Context() context.Context { return b.ctx }

// retainedBytes includes all variable-size envelope fields, including fields that
// are not meaningful for this message type but can still arrive from a peer.
func retainedBytes(m protocol.Message) int {
	return 512 + len(m.Type) + len(m.Role) + len(m.DeviceID) + len(m.TargetID) + len(m.Secret) + len(m.Grant) + len(m.AccountToken) + len(m.RequestID) +
		len(m.NewGrantHash) + len(m.OS) + len(m.Arch) + len(m.AgentVersion) + len(m.Workspace) + len(m.ExecID) +
		len(m.Op) + len(m.Payload) + len(m.Code) + len(m.Data) + len(m.Status)
}

func (b *outbox) Enqueue(m protocol.Message) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closing || b.ctx.Err() != nil {
		return errOutboxClosed
	}
	size := retainedBytes(m)
	if outputMessage(m) {
		if b.buffered[m.ExecID]+size > protocol.MaxRelayBufferBytes ||
			len(b.queue) >= protocol.MaxRelayQueueFrames-protocol.MaxConcurrentExec*2 ||
			b.bytes+size > protocol.MaxRelayQueueBytes-protocol.MaxEnvelopeBytes*protocol.MaxConcurrentExec {
			return errOutputDropped
		}
	} else if len(b.queue) >= protocol.MaxRelayQueueFrames || b.bytes+size > protocol.MaxRelayQueueBytes {
		// A terminal may evict its own queued output, but never reorder another flow.
		if m.Type == protocol.TypeExit || m.Type == protocol.TypeError || m.Type == protocol.TypeInvokeNack {
			if b.discardOutput(m.ExecID) && m.Type == protocol.TypeExit {
				m.Truncated = protocol.Bool(true)
			}
		}
		if len(b.queue) >= protocol.MaxRelayQueueFrames || b.bytes+size > protocol.MaxRelayQueueBytes {
			b.closing = true
			b.cancel()
			return errOutboxClosed
		}
	}
	b.queue = append(b.queue, queuedMessage{message: m, bytes: size})
	b.bytes += size
	if outputMessage(m) {
		b.buffered[m.ExecID] += size
	}
	b.signal()
	return nil
}

func (b *outbox) discardOutput(id string) bool {
	dropped := false
	kept := b.queue[:0]
	for _, item := range b.queue {
		if outputMessage(item.message) && item.message.ExecID == id {
			b.bytes -= item.bytes
			b.buffered[id] -= item.bytes
			dropped = true
		} else {
			kept = append(kept, item)
		}
	}
	clear(b.queue[len(kept):])
	b.queue = kept
	if b.buffered[id] == 0 {
		delete(b.buffered, id)
	}
	return dropped
}

func (b *outbox) signal() {
	select {
	case b.wake <- struct{}{}:
	default:
	}
}

// Close requests a bounded drain. It is safe under the Hub mutex.
func (b *outbox) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closing {
		return
	}
	b.closing = true
	b.timer = time.AfterFunc(protocol.RelayWriteTimeout, b.cancel)
	b.signal()
}

// shutdown is only called by the connection owner, outside the Hub mutex.
func (b *outbox) shutdown() {
	b.Close()
	<-b.done
}

func (b *outbox) run() {
	defer func() {
		b.cancel()
		b.disconnect()
		b.mu.Lock()
		b.closing = true
		if b.timer != nil {
			b.timer.Stop()
		}
		b.queue = nil
		b.buffered = nil
		b.bytes = 0
		b.mu.Unlock()
		close(b.done)
	}()
	for {
		if b.ctx.Err() != nil {
			return
		}
		b.mu.Lock()
		if len(b.queue) == 0 {
			closing := b.closing
			b.mu.Unlock()
			if closing {
				return
			}
			select {
			case <-b.wake:
			case <-b.ctx.Done():
				return
			}
			continue
		}
		item := b.queue[0]
		b.queue[0] = queuedMessage{}
		b.queue = b.queue[1:]
		b.mu.Unlock()
		ctx, cancel := context.WithTimeout(b.ctx, protocol.RelayWriteTimeout)
		err := b.write(ctx, item.message)
		cancel()
		b.mu.Lock()
		b.bytes -= item.bytes
		if outputMessage(item.message) {
			b.buffered[item.message.ExecID] -= item.bytes
			if b.buffered[item.message.ExecID] == 0 {
				delete(b.buffered, item.message.ExecID)
			}
		}
		b.mu.Unlock()
		if err != nil {
			return
		}
	}
}
