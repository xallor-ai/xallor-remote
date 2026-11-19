package noderun

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/accesscode"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

const sessionApprovalWait = 60 * time.Second

type sessionApprovalHub struct {
	mu      sync.Mutex
	subs    map[frameWriter]struct{}
	pending map[string]*pendingSessionApproval
	seq     atomic.Uint64
}

type pendingSessionApproval struct {
	request accesscode.SessionRequest
	digest  string
	created string
	expires string
	ch      chan sessionDecision
}

type sessionDecision struct {
	allow bool
	code  string
}

type sessionApprovalView struct {
	ID            string   `json:"id"`
	RequestID     string   `json:"request_id"`
	SessionID     string   `json:"session_id"`
	CodeID        string   `json:"code_id"`
	Generation    uint64   `json:"generation"`
	Scope         []string `json:"scope"`
	RequestSHA256 string   `json:"request_sha256"`
	Created       string   `json:"created"`
	Expires       string   `json:"expires"`
}

func newSessionApprovalHub() *sessionApprovalHub {
	return &sessionApprovalHub{subs: map[frameWriter]struct{}{}, pending: map[string]*pendingSessionApproval{}}
}

func (h *sessionApprovalHub) subscribe(c frameWriter) {
	h.mu.Lock()
	h.subs[c] = struct{}{}
	h.mu.Unlock()
}

func (h *sessionApprovalHub) unsubscribe(c frameWriter) {
	h.mu.Lock()
	delete(h.subs, c)
	var cancelled []*pendingSessionApproval
	if len(h.subs) == 0 {
		for id, pending := range h.pending {
			delete(h.pending, id)
			cancelled = append(cancelled, pending)
		}
	}
	h.mu.Unlock()
	for _, pending := range cancelled {
		select {
		case pending.ch <- sessionDecision{code: protocol.Cancelled}:
		default:
		}
	}
}

func (h *sessionApprovalHub) ask(ctx context.Context, request accesscode.SessionRequest) (bool, string) {
	digest, err := request.Digest()
	if err != nil {
		return false, protocol.PolicyDeny
	}
	h.mu.Lock()
	if len(h.subs) == 0 {
		h.mu.Unlock()
		return false, protocol.PolicyDeny
	}
	id := "s" + itoa(h.seq.Add(1))
	now := time.Now().UTC()
	pending := &pendingSessionApproval{request: request, digest: digest, created: now.Format(time.RFC3339), expires: now.Add(sessionApprovalWait).Format(time.RFC3339), ch: make(chan sessionDecision, 1)}
	h.pending[id] = pending
	subs := make([]frameWriter, 0, len(h.subs))
	for c := range h.subs {
		subs = append(subs, c)
	}
	h.mu.Unlock()
	waitContext, cancelWait := context.WithDeadline(ctx, now.Add(sessionApprovalWait))
	defer cancelWait()
	defer h.clear(id)
	frame := ipc.EventParams(id, "session_approval", map[string]any{
		"session_id": request.SessionID, "request_id": request.RequestID, "code_id": request.CodeID,
		"generation": request.Generation, "scope": request.Scope, "request_sha256": digest,
		"created": pending.created, "expires": pending.expires,
	})
	for _, sub := range subs {
		if err := writeApproval(waitContext, sub, frame); err != nil {
			if waitContext.Err() != nil {
				break
			}
			h.unsubscribe(sub)
		}
	}
	select {
	case <-waitContext.Done():
		return false, approvalContextCode(ctx)
	case decision := <-pending.ch:
		if waitContext.Err() != nil {
			return false, approvalContextCode(ctx)
		}
		if decision.allow {
			return true, ""
		}
		if decision.code != "" {
			return false, decision.code
		}
		return false, protocol.PolicyDeny
	}
}

func (h *sessionApprovalHub) respond(id, digest string, allow bool) bool {
	h.mu.Lock()
	pending := h.pending[id]
	if pending != nil && pending.digest != digest {
		pending = nil
	}
	if pending != nil {
		delete(h.pending, id)
	}
	h.mu.Unlock()
	if pending == nil {
		return false
	}
	select {
	case pending.ch <- sessionDecision{allow: allow}:
		return true
	default:
		return false
	}
}

func (h *sessionApprovalHub) clear(id string) {
	h.mu.Lock()
	delete(h.pending, id)
	h.mu.Unlock()
}

func (h *sessionApprovalHub) list() []sessionApprovalView {
	h.mu.Lock()
	defer h.mu.Unlock()
	items := make([]sessionApprovalView, 0, len(h.pending))
	for id, pending := range h.pending {
		items = append(items, sessionApprovalView{ID: id, RequestID: pending.request.RequestID, SessionID: pending.request.SessionID, CodeID: pending.request.CodeID, Generation: pending.request.Generation, Scope: append([]string(nil), pending.request.Scope...), RequestSHA256: pending.digest, Created: pending.created, Expires: pending.expires})
	}
	return items
}
