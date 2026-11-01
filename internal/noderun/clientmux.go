package noderun

import (
	"sync"

	"github.com/coder/websocket"

	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

const waiterDataSlots = 16

type execWaiter struct {
	ch    chan protocol.Message
	done  chan struct{}
	mu    sync.Mutex
	term  *protocol.Message
	trunc bool
}

func newExecWaiter() *execWaiter {
	return &execWaiter{
		ch:   make(chan protocol.Message, waiterDataSlots),
		done: make(chan struct{}),
	}
}

func isTerminal(msg protocol.Message) bool {
	switch msg.Type {
	case protocol.TypeExit, protocol.TypeInvokeNack, protocol.TypeError:
		return true
	default:
		return false
	}
}

func (w *execWaiter) push(msg protocol.Message) {
	if isTerminal(msg) {
		w.finish(msg)
		return
	}
	select {
	case w.ch <- msg:
	default:
		w.mu.Lock()
		w.trunc = true
		w.mu.Unlock()
	}
}

func (w *execWaiter) finish(msg protocol.Message) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.term != nil {
		return
	}
	if w.trunc && msg.Type == protocol.TypeExit {
		msg.Truncated = protocol.Bool(true)
	}
	m := msg
	w.term = &m
	close(w.done)
}

func (w *execWaiter) next() (protocol.Message, bool) {
	select {
	case msg := <-w.ch:
		return msg, true
	case <-w.done:
		select {
		case msg := <-w.ch:
			return msg, true
		default:
			w.mu.Lock()
			t := w.term
			w.term = nil
			w.mu.Unlock()
			if t == nil {
				return protocol.Message{}, false
			}
			return *t, true
		}
	}
}

func (cl *clientLink) subscribe(id string) *execWaiter {
	w := newExecWaiter()
	cl.mu.Lock()
	if cl.waiters == nil {
		cl.waiters = map[string]*execWaiter{}
	}
	cl.waiters[id] = w
	cl.mu.Unlock()
	return w
}

func (cl *clientLink) unsubscribe(id string) {
	cl.mu.Lock()
	delete(cl.waiters, id)
	cl.mu.Unlock()
}

func (cl *clientLink) dispatch(msg protocol.Message) {
	if msg.ExecID == "" {
		return
	}
	cl.mu.Lock()
	w := cl.waiters[msg.ExecID]
	cl.mu.Unlock()
	if w != nil {
		w.push(msg)
	}
}

func (cl *clientLink) failWaiters() {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	for _, w := range cl.waiters {
		w.finish(protocol.Message{Type: protocol.TypeError, Code: protocol.RelayError})
	}
}

func (cl *clientLink) closeConn() {
	if cl.cancel != nil {
		cl.cancel()
	}
	if cl.ws != nil && cl.ws.conn != nil {
		_ = cl.ws.conn.Close(websocket.StatusNormalClosure, "")
	}
}
