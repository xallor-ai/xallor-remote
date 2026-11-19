package relay

import (
	"context"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/accesscode"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Publish only into the same live routing incarnation observed before commit.
// Enqueue is nonblocking by Sender contract; keeping it under the routing lock
// orders the accepted reply and binding before a subsequent Drop/replacement.
func (h *Hub) publishResumedSession(ctx context.Context, from Sender, expected clientSlot, device *deviceSlot, connection Sender, session *accesscode.SessionAuthorization, resume *accesscode.ResumeCapability, result protocol.Message) bool {
	// Resume commits on the client connection while owner code management runs
	// on the device connection. Order publication with that management gate and
	// re-read after acquiring it: a successful consume can already be revoked.
	// Keep SQL outside h.mu, matching management's gate -> SQL -> routing order.
	release, err := h.identities.acquire(ctx, expected.targetID)
	if err != nil {
		return false
	}
	defer release()
	var currentSession accesscode.SessionAuthorization
	if err := h.accessCodes.ReadAccessCode(ctx, func(reader accesscode.Reader) error {
		var err error
		currentSession, err = reader.Session(ctx, session.ID)
		return err
	}); err != nil || !sameExecutionConsent(currentSession, *session) {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	current, ok := h.clients[from]
	actual := h.devices[expected.targetID]
	if !ok || ctx.Err() != nil || actual != device || actual == nil || actual.conn != connection || connection == nil || connection.Context().Err() != nil || !actual.online || !actual.ready || !actual.record.Inbound {
		return false
	}
	if current.targetID != expected.targetID || current.access != expected.access || current.identity != expected.identity || current.authorization != expected.authorization || current.session != expected.session || current.sessionRequired != expected.sessionRequired || !actual.record.Binding().Matches(expected.access) {
		return false
	}
	if !session.Target.Valid() || session.Target != actual.sessionTarget {
		return false
	}
	now := time.Now().UTC()
	if currentSession.CanOperate(now) != nil || !now.Before(resume.ExpiresAt) || (current.identity.CredentialID != "" && current.identity.ExpiresAt <= now.Unix()) || (current.authorization.ExpiresAt != 0 && current.authorization.ExpiresAt <= now.Unix()) {
		return false
	}
	if from.Enqueue(result) != nil {
		return false
	}
	current.session, current.sessionRequired = session, true
	h.clients[from] = current
	return true
}

// This is compensation for a confirmed commit whose result was not admitted
// to the connection. It never restores a consumed capability. Failure leaves
// an unknown outcome, not a claimed rollback or successful cleanup.
func (h *Hub) retireUnpublishedSession(sessionID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), protocol.RelayIdentityTimeout)
	defer cancel()
	return h.accessCodes.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		session, err := tx.Session(ctx, sessionID)
		if err != nil {
			return err
		}
		if session.State != accesscode.SessionActive {
			return nil
		}
		_, err = tx.RevokeSession(ctx, sessionID, time.Now().UTC())
		return err
	})
}
