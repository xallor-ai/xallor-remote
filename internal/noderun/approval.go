package noderun

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

const approvalWait = 60 * time.Second

type approvalHub struct {
	mu      sync.Mutex
	subs    map[frameWriter]struct{}
	pending map[string]*pendingApproval // key = approval request id
	byExec  map[string]string           // exec_id → approval id
	seq     atomic.Uint64
}

type frameWriter interface {
	WriteFrame(ipc.Frame) error
}

type pendingApproval struct {
	execID string
	ch     chan decide
}

type decide struct {
	allow bool
	code  string // if !allow: policy_deny | approval_timeout | cancelled
}

func newApprovalHub() *approvalHub {
	return &approvalHub{
		subs:    map[frameWriter]struct{}{},
		pending: map[string]*pendingApproval{},
		byExec:  map[string]string{},
	}
}

func (h *approvalHub) subscribe(c frameWriter) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.subs[c] = struct{}{}
}

func (h *approvalHub) unsubscribe(c frameWriter) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.subs, c)
}

// ask broadcasts to human UI heads. No subscriber → policy_deny without waiting.
func (h *approvalHub) ask(execID, preview string) (allow bool, code string) {
	h.mu.Lock()
	if len(h.subs) == 0 {
		h.mu.Unlock()
		return false, protocol.PolicyDeny
	}
	id := "a" + itoa(h.seq.Add(1))
	p := &pendingApproval{execID: execID, ch: make(chan decide, 1)}
	h.pending[id] = p
	h.byExec[execID] = id
	subs := make([]frameWriter, 0, len(h.subs))
	for c := range h.subs {
		subs = append(subs, c)
	}
	h.mu.Unlock()

	params := map[string]string{"exec_id": execID, "preview": preview}
	fr := ipc.EventParams(id, "approval", params)
	for _, c := range subs {
		_ = c.WriteFrame(fr)
	}

	timer := time.NewTimer(approvalWait)
	defer timer.Stop()
	select {
	case d := <-p.ch:
		h.clear(id, execID)
		if d.allow {
			return true, ""
		}
		if d.code == "" {
			return false, protocol.PolicyDeny
		}
		return false, d.code
	case <-timer.C:
		h.clear(id, execID)
		return false, protocol.ApprovalTimeout
	}
}

func (h *approvalHub) respond(id string, allow bool) bool {
	h.mu.Lock()
	p := h.pending[id]
	h.mu.Unlock()
	if p == nil {
		return false
	}
	code := protocol.PolicyDeny
	if allow {
		code = ""
	}
	select {
	case p.ch <- decide{allow: allow, code: code}:
		return true
	default:
		return false
	}
}

func (h *approvalHub) cancelExec(execID string) {
	h.mu.Lock()
	id, ok := h.byExec[execID]
	p := h.pending[id]
	h.mu.Unlock()
	if !ok || p == nil {
		return
	}
	select {
	case p.ch <- decide{allow: false, code: protocol.Cancelled}:
	default:
	}
}

func (h *approvalHub) clear(id, execID string) {
	h.mu.Lock()
	delete(h.pending, id)
	if h.byExec[execID] == id {
		delete(h.byExec, execID)
	}
	h.mu.Unlock()
}

func itoa(n uint64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
