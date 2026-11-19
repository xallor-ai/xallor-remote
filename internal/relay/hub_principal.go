package relay

import (
	"context"
	"errors"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/accesscode"
	"github.com/xallor-ai/xallor-remote/internal/principal"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

func (h *Hub) verifySessionAccess(from Sender, sessionID, operation string) string {
	h.mu.Lock()
	client, ok := h.clients[from]
	store := h.accessCodes
	h.mu.Unlock()
	if !ok {
		return protocol.Unauthorized
	}
	if !client.sessionRequired {
		if sessionID != "" {
			return protocol.Unauthorized
		}
		return ""
	}
	if sessionID == "" || store == nil {
		return protocol.SessionPending
	}
	if client.session == nil || client.session.ID != sessionID {
		return protocol.SessionPending
	}
	ctx, cancel := context.WithTimeout(from.Context(), protocol.RelayIdentityTimeout)
	defer cancel()
	var result string
	var expired bool
	err := store.ReadAccessCode(ctx, func(reader accesscode.Reader) error {
		session, err := reader.Session(ctx, sessionID)
		if err != nil || session.ID != sessionID || !session.Allows(operation) {
			result = protocol.Unauthorized
			return nil
		}
		if err := session.CanOperate(time.Now().UTC()); err != nil {
			result = protocol.Unauthorized
			if errors.Is(err, accesscode.ErrSessionExpired) {
				expired = true
			}
		}
		return nil
	})
	if err != nil {
		return protocol.RelayError
	}
	if expired {
		_ = store.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
			_, err := tx.EndSession(ctx, sessionID, time.Now().UTC())
			return err
		})
	}
	return result
}

// A new invocation revalidates the authority-issued credential and membership.
// This database wait is outside the routing mutex and is never on output frames.
// Cancellation remains available to the original connection even after revocation.
func (h *Hub) verifyClientAccess(from Sender, operation string) string {
	h.mu.Lock()
	client, ok := h.clients[from]
	h.mu.Unlock()
	if !ok {
		return protocol.Unauthorized
	}
	ctx, cancel := context.WithTimeout(from.Context(), protocol.RelayIdentityTimeout)
	defer cancel()
	if client.access.OwnershipID != "" {
		if h.accessService == nil {
			return protocol.Unauthorized
		}
		if err := h.accessService.Verify(ctx, client.identity, client.authorization, operation); err != nil {
			return accessErrorCode(err)
		}
		return ""
	}
	if client.identity.PrincipalID == "" {
		return ""
	}
	if h.accounts == nil {
		return protocol.Unauthorized
	}
	if err := h.accounts.Verify(ctx, client.identity); err != nil {
		return principalErrorCode(err)
	}
	return ""
}

func principalErrorCode(err error) string {
	if errors.Is(err, principal.ErrUnauthorized) {
		return protocol.Unauthorized
	}
	return protocol.RelayError
}
