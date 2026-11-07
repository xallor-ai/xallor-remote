package noderun

import (
	"sync"

	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

type deviceWaiter struct {
	link   *wsConn
	result chan protocol.Message
}
type deviceReplies struct {
	mu      sync.Mutex
	pending map[string]deviceWaiter
}

func (r *deviceReplies) register(link *wsConn) (string, <-chan protocol.Message, func(), error) {
	id, err := protocol.RandomHex(16)
	if err != nil {
		return "", nil, nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.pending) >= 16 {
		return "", nil, nil, errStr(protocol.QuotaExceeded)
	}
	if r.pending == nil {
		r.pending = map[string]deviceWaiter{}
	}
	result := make(chan protocol.Message, 1)
	r.pending[id] = deviceWaiter{link: link, result: result}
	return id, result, func() { r.mu.Lock(); delete(r.pending, id); r.mu.Unlock() }, nil
}

func (r *deviceReplies) deliver(link *wsConn, msg protocol.Message) {
	r.mu.Lock()
	defer r.mu.Unlock()
	waiter, ok := r.pending[msg.RequestID]
	if !ok || waiter.link != link {
		return
	}
	select {
	case waiter.result <- msg:
	default:
	}
}
