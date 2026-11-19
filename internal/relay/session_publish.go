package relay

import (
	"context"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/accesscode"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

func (h *Hub) publishApprovedSession(ctx context.Context, pending *pendingSession, expected clientSlot, session *accesscode.SessionAuthorization, resume *accesscode.ResumeCapability, result protocol.Message) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	device := h.devices[pending.targetID]
	client, ok := h.clients[pending.client]
	if !ok || client != expected || h.pendingSessions[session.ID] != pending || ctx.Err() != nil || pending.client.Context().Err() != nil || device != pending.device || device == nil || device.conn != pending.connection || pending.connection.Context().Err() != nil || device.sessionTarget != session.Target || !session.Target.Valid() || !device.online || !device.ready || !device.record.Inbound {
		return false
	}
	if !device.record.Binding().Matches(expected.access) {
		return false
	}
	now := time.Now().UTC()
	if session.CanOperate(now) != nil || resume == nil || !now.Before(resume.ExpiresAt) || (client.identity.CredentialID != "" && client.identity.ExpiresAt <= now.Unix()) || (client.authorization.ExpiresAt != 0 && client.authorization.ExpiresAt <= now.Unix()) {
		return false
	}
	if pending.client.Enqueue(result) != nil {
		return false
	}
	client.session, client.sessionRequired = session, true
	h.clients[pending.client] = client
	delete(h.pendingSessions, session.ID)
	return true
}

func (h *Hub) finishRejectedSession(pending *pendingSession, expected clientSlot, result protocol.Message, closeOwned bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	owned := false
	if h.pendingSessions[pending.request.SessionID] == pending {
		delete(h.pendingSessions, pending.request.SessionID)
		if client, ok := h.clients[pending.client]; ok && client == expected {
			owned = true
			client.sessionRequired = true
			h.clients[pending.client] = client
		}
	}
	_ = pending.client.Enqueue(result)
	// Ownership and close must share the routing lock: a later operation on
	// this connection cannot be closed by an old publication failure.
	if closeOwned && owned {
		pending.client.Close()
	}
}
