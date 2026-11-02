package relay

import (
	"context"
	"errors"
	"github.com/xallor-ai/xallor-remote/internal/access"

	"github.com/xallor-ai/xallor-remote/internal/principal"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

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
		store, ok := h.store.(access.Store)
		if !ok {
			return protocol.Unauthorized
		}
		if err := store.VerifyAccess(ctx, client.identity, client.authorization, operation); err != nil {
			return accessErrorCode(err)
		}
		return ""
	}
	if client.identity.PrincipalID == "" {
		return ""
	}
	auth, ok := h.store.(principal.Authenticator)
	if !ok {
		return protocol.Unauthorized
	}
	if err := auth.Verify(ctx, client.identity); err != nil {
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
