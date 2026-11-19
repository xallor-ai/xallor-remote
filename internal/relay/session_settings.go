package relay

import (
	"github.com/xallor-ai/xallor-remote/internal/accesscode"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Called with h.mu held after a committed settings/epoch mutation. Preserve
// controller connections and inflight streams until their actual terminal;
// revoke only consent and pending admission, never pretend the process exited.
func (h *Hub) invalidateSessionConsentLocked(id string, retired accesscode.TargetBinding) {
	for key, pending := range h.pendingSessions {
		if pending.targetID != id || pending.target != retired {
			continue
		}
		delete(h.pendingSessions, key)
		_ = pending.client.Enqueue(protocol.Message{Type: protocol.TypeSessionResult, RequestID: pending.request.RequestID, SessionID: pending.request.SessionID, RequestSHA256: pending.digest, Status: protocol.SessionUnknownState, Code: protocol.SessionUnknown})
	}
	for conn, client := range h.clients {
		if client.targetID != id || (!client.sessionRequired && client.session == nil) {
			continue
		}
		if client.session != nil && client.session.Target != retired {
			continue
		}
		client.session, client.sessionRequired = nil, true
		h.clients[conn] = client
	}
}

// A sticky session-mode connection can explicitly seek new consent once its
// previous session is retired; two pending requests still cannot share it.
func (h *Hub) hasPendingSessionForClientLocked(client Sender) bool {
	for _, pending := range h.pendingSessions {
		if pending.client == client {
			return true
		}
	}
	return false
}
