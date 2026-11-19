package relay

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/accesscode"
	"github.com/xallor-ai/xallor-remote/internal/delegation"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

func sameSessionOrigin(a, b accesscode.SessionAuthorization) bool {
	return a.ID == b.ID && a.CodeID == b.CodeID && a.Generation == b.Generation && a.Target.Valid() && a.Target == b.Target
}

func sameSessionDeadline(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}

func sameExecutionConsent(a, b accesscode.SessionAuthorization) bool {
	return sameSessionOrigin(a, b) && slices.Equal(a.Scope, b.Scope) && sameSessionDeadline(a.ExpiresAt, b.ExpiresAt)
}

// Called under h.mu for a known session denial/expiry. Keep the shared
// connection and old route, but allow an explicit new approval without a
// fallback to transport grants or clearing a newly adopted session.
func (h *Hub) detachDeniedExecutionConsentLocked(route *inflight) {
	current, ok := h.clients[route.client]
	if !ok || current.session == nil || route.binding.session == nil || !sameExecutionConsent(*current.session, *route.binding.session) {
		return
	}
	current.session, current.sessionRequired = nil, true
	h.clients[route.client] = current
}

// An inflight route retains its original approved session even if the shared
// controller later obtains another session. Check that immutable consent, not
// the controller's latest session pointer or the original access-code expiry.
func (h *Hub) executionSession(ctx context.Context, binding clientSlot) (*accesscode.SessionAuthorization, error) {
	if binding.session == nil {
		if binding.sessionRequired {
			return nil, delegation.ErrDenied
		}
		return nil, nil
	}
	if h.accessCodes == nil {
		return nil, delegation.ErrDenied
	}
	var current accesscode.SessionAuthorization
	err := h.accessCodes.ReadAccessCode(ctx, func(reader accesscode.Reader) error {
		var err error
		current, err = reader.Session(ctx, binding.session.ID)
		return err
	})
	if errors.Is(err, accesscode.ErrSessionState) {
		return nil, delegation.ErrDenied
	}
	if err != nil {
		return nil, err
	}
	if !sameSessionOrigin(current, *binding.session) {
		return nil, delegation.ErrDenied
	}
	if err := current.CanOperate(time.Now().UTC()); err != nil {
		if errors.Is(err, accesscode.ErrSessionExpired) {
			// Preserve the known denial before any best-effort persistence wait.
			return &current, delegation.ErrDenied
		}
		return nil, delegation.ErrDenied
	}
	if !current.Allows(protocol.OpExec) || !sameExecutionConsent(current, *binding.session) {
		return nil, delegation.ErrDenied
	}
	return &current, nil
}

// This is a best-effort durable terminal after proven expiry, not permission
// to end a replacement/extended authorization and not proof of process cleanup.
func (h *Hub) endExpiredExecutionSession(ctx context.Context, expected accesscode.SessionAuthorization) {
	_ = h.accessCodes.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		current, err := tx.Session(ctx, expected.ID)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		if !sameSessionOrigin(current, expected) || !errors.Is(current.CanOperate(now), accesscode.ErrSessionExpired) {
			return nil
		}
		_, err = tx.EndSession(ctx, current.ID, now)
		return err
	})
}

// Clamp again under the final routing lock. Neither later SQL/routing waits
// nor millisecond rounding can extend the provider/session authorization.
func sessionExecutionLeaseMS(deadline time.Time, session *accesscode.SessionAuthorization, now time.Time) int64 {
	if session != nil {
		if session.CanOperate(now) != nil {
			return 0
		}
		if session.ExpiresAt != nil && session.ExpiresAt.Before(deadline) {
			deadline = *session.ExpiresAt
		}
	}
	remaining := deadline.Sub(now)
	if remaining <= 0 {
		return 0
	}
	return remaining.Milliseconds()
}
