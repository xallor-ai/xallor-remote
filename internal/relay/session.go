package relay

import (
	"context"
	"errors"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/accesscode"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

const sessionReservationTimeout = time.Minute
const sessionResumeTTL = 10 * time.Minute

func (h *Hub) handleSessionRequest(from Sender, targetID string, msg protocol.Message) {
	reply := func(status, code, sessionID, digest string) {
		_ = from.Enqueue(protocol.Message{Type: protocol.TypeSessionResult, RequestID: msg.RequestID, SessionID: sessionID, Status: status, Code: code, RequestSHA256: digest})
	}
	if h.accessCodes == nil || msg.Grant == "" || len(msg.RequestID) > 128 {
		reply(protocol.SessionRejectedState, protocol.Unauthorized, msg.SessionID, "")
		return
	}
	request, err := msg.AccessCodeSessionRequest()
	if err != nil {
		reply(protocol.SessionRejectedState, protocol.AgentError, msg.SessionID, "")
		return
	}
	digest, err := request.Digest()
	if err != nil {
		reply(protocol.SessionRejectedState, protocol.AgentError, request.SessionID, "")
		return
	}
	h.mu.Lock()
	client, clientOK := h.clients[from]
	device := h.devices[targetID]
	if existing := h.pendingSessions[request.SessionID]; existing != nil && existing.client == from && existing.digest == digest {
		reply(protocol.SessionPending, protocol.SessionPending, request.SessionID, digest)
		h.mu.Unlock()
		return
	}
	if !clientOK || client.targetID != targetID || client.session != nil || h.hasPendingSessionForClientLocked(from) || h.pendingSessions[request.SessionID] != nil || device == nil || !device.online || !device.ready || device.conn == nil || request.TargetUID != device.record.UID {
		h.mu.Unlock()
		reply(protocol.SessionRejectedState, protocol.Unauthorized, request.SessionID, digest)
		return
	}
	deviceConn := device.conn
	targetUID := device.record.UID
	targetOwnership := device.record.OwnershipID
	expectedClient := client
	expectedDevice := device
	target := device.sessionTarget
	h.mu.Unlock()
	ctx, cancel := context.WithTimeout(from.Context(), protocol.RelayIdentityTimeout)
	defer cancel()
	reservationID := request.RequestID
	if err := h.accessCodes.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		code, err := tx.Code(ctx, request.CodeID)
		if err != nil {
			return err
		}
		if (code.DeviceUID != "" && code.DeviceUID != targetUID) || (code.OwnershipID != "" && code.OwnershipID != targetOwnership) {
			return accesscode.ErrCodeSecret
		}
		if _, err := code.ScopeForSession(request.Generation, request.Scope); err != nil {
			return err
		}
		_, _, err = tx.ReserveCode(ctx, request.CodeID, protocol.SHA256Hex(msg.Grant), reservationID, request.SessionID, time.Now().UTC(), time.Now().UTC().Add(sessionReservationTimeout), target)
		return err
	}); err != nil {
		code, state := sessionCode(err), protocol.SessionRejectedState
		if code == protocol.SessionUnknown {
			state = protocol.SessionUnknownState
		}
		reply(state, code, request.SessionID, digest)
		return
	}
	h.mu.Lock()
	client, clientOK = h.clients[from]
	device = h.devices[targetID]
	if !clientOK || client != expectedClient || h.hasPendingSessionForClientLocked(from) || h.pendingSessions[request.SessionID] != nil || device != expectedDevice || device == nil || device.conn != deviceConn || device.sessionTarget != target || !device.record.Binding().Matches(expectedClient.access) || !device.online || !device.ready || !device.record.Inbound || ctx.Err() != nil || deviceConn.Context().Err() != nil {
		h.mu.Unlock()
		_ = h.releaseSessionReservation(context.Background(), reservationID, request.SessionID)
		reply(protocol.SessionUnknownState, protocol.SessionUnknown, request.SessionID, digest)
		return
	}
	client.sessionRequired = true
	h.clients[from] = client
	pending := &pendingSession{request: request, digest: digest, client: from, targetID: targetID, reservationID: reservationID, target: target, device: device, connection: deviceConn}
	h.pendingSessions[request.SessionID] = pending
	approval := protocol.Message{Type: protocol.TypeSessionApprovalRequest, RequestID: request.RequestID, SessionID: request.SessionID, TargetID: targetID, TargetUID: targetUID, CodeID: request.CodeID, CodeGeneration: request.Generation, SessionScope: append([]string(nil), request.Scope...), SessionExpiresAt: msg.SessionExpiresAt, RequestSHA256: digest}
	if err := deviceConn.Enqueue(approval); err != nil {
		delete(h.pendingSessions, request.SessionID)
		if client, ok := h.clients[from]; ok {
			client.sessionRequired = true
			h.clients[from] = client
		}
		h.mu.Unlock()
		_ = h.releaseSessionReservation(context.Background(), reservationID, request.SessionID)
		reply(protocol.SessionUnknownState, protocol.SessionUnknown, request.SessionID, digest)
		return
	}
	reply(protocol.SessionPending, protocol.SessionPending, request.SessionID, digest)
	h.mu.Unlock()
}

func (h *Hub) handleSessionApproval(from Sender, targetID string, msg protocol.Message) {
	if err := protocol.ValidateSessionApprovalResult(msg); err != nil {
		_ = from.Enqueue(protocol.Message{Type: protocol.TypeError, Code: protocol.AgentError})
		return
	}
	h.mu.Lock()
	pending := h.pendingSessions[msg.SessionID]
	device := h.devices[targetID]
	if pending == nil || pending.targetID != targetID || device == nil || device.conn != from || device != pending.device || device.sessionTarget != pending.target || pending.connection != from || pending.request.RequestID != msg.RequestID || pending.digest != msg.RequestSHA256 {
		h.mu.Unlock()
		_ = from.Enqueue(protocol.Message{Type: protocol.TypeError, Code: protocol.Unauthorized})
		return
	}
	h.mu.Unlock()
	ctx, cancel := context.WithTimeout(from.Context(), protocol.RelayIdentityTimeout)
	defer cancel()
	var resultCode = protocol.SessionRejected
	var resultState = protocol.SessionRejectedState
	var session *accesscode.SessionAuthorization
	var resume *accesscode.ResumeCapability
	var resumeSecret string
	h.mu.Lock()
	clientSlot, clientSlotOK := h.clients[pending.client]
	h.mu.Unlock()
	resumeKind, resumeBinding := sessionResumeBinding(clientSlot, clientSlotOK)
	err := h.accessCodes.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		session, resume, resumeSecret = nil, nil, ""
		reservation, err := tx.Reservation(ctx, pending.reservationID)
		if err != nil {
			return err
		}
		if reservation.Target != pending.target {
			return accesscode.ErrTargetEpoch
		}
		if msg.Allow != nil && *msg.Allow {
			authority, err := tx.ResumeAuthority(ctx, targetID, clientSlot.identity.CredentialID)
			if err != nil {
				return err
			}
			kind, digest := authority.Digest()
			if resumeKind == "" || kind != resumeKind || digest != resumeBinding {
				return accesscode.ErrResumeBinding
			}
		}
		_, _, created, err := tx.FinalizeReservation(ctx, pending.reservationID, msg.Allow != nil && *msg.Allow, time.Now().UTC(), pending.request.SessionExpiresAt, pending.request.Scope)
		if err == nil && created.ID != "" {
			session = &created
			if resumeKind != "" {
				secret, randomErr := protocol.RandomHex(32)
				if randomErr != nil {
					return randomErr
				}
				now := time.Now().UTC()
				expires := now.Add(sessionResumeTTL)
				if created.ExpiresAt != nil && created.ExpiresAt.Before(expires) {
					expires = *created.ExpiresAt
				}
				resumeID, randomErr := protocol.RandomHex(16)
				if randomErr != nil {
					return randomErr
				}
				candidate := accesscode.ResumeCapability{ID: resumeID, SessionID: created.ID, BindingKind: resumeKind, BindingDigest: resumeBinding, CapabilityHash: protocol.SHA256Hex(secret), IssuedAt: now, ExpiresAt: expires, Generation: created.Generation, State: accesscode.ResumeActive}
				if err := tx.InsertResume(ctx, candidate); err != nil {
					return err
				}
				resume, resumeSecret = &candidate, secret
			}
		}
		return err
	})
	if err == nil && session != nil {
		resultCode, resultState = "", protocol.SessionActive
	} else if err != nil {
		session, resume, resumeSecret = nil, nil, ""
		resultCode, resultState = protocol.SessionUnknown, protocol.SessionUnknownState
		_ = h.markSessionUnknown(context.Background(), pending.reservationID)
	}
	result := protocol.Message{
		Type: protocol.TypeSessionResult, RequestID: msg.RequestID, SessionID: msg.SessionID,
		Status: resultState, Code: resultCode, RequestSHA256: msg.RequestSHA256,
		CodeGeneration: pending.request.Generation, SessionScope: append([]string(nil), pending.request.Scope...),
		SessionExpiresAt: sessionExpiryUnix(pending.request.SessionExpiresAt),
	}
	if session != nil {
		result.CodeGeneration = session.Generation
		result.SessionScope = append([]string(nil), session.Scope...)
		result.SessionExpiresAt = sessionExpiryUnix(session.ExpiresAt)
	}
	if resume != nil {
		result.ResumeID = resume.ID
		result.ResumeCapability = resumeSecret
	}
	if session != nil {
		if !h.publishApprovedSession(ctx, pending, clientSlot, session, resume, result) {
			_ = h.retireUnpublishedSession(session.ID)
			result.Status, result.Code = protocol.SessionUnknownState, protocol.SessionUnknown
			result.ResumeID, result.ResumeCapability = "", ""
			h.finishRejectedSession(pending, clientSlot, result, true)
		}
	} else {
		h.finishRejectedSession(pending, clientSlot, result, false)
	}
	deviceResult := result
	deviceResult.ResumeID = ""
	deviceResult.ResumeCapability = ""
	_ = from.Enqueue(deviceResult)
}

func sessionExpiryUnix(expiresAt *time.Time) *int64 {
	if expiresAt == nil {
		return nil
	}
	value := expiresAt.UTC().Unix()
	return &value
}

func (h *Hub) releaseSessionReservation(ctx context.Context, reservationID, sessionID string) error {
	return h.accessCodes.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		_, _, _, err := tx.FinalizeReservation(ctx, reservationID, false, time.Now().UTC(), nil, nil)
		return err
	})
}

func (h *Hub) markSessionUnknown(ctx context.Context, reservationID string) error {
	return h.accessCodes.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		_, err := tx.MarkReservationUnknown(ctx, reservationID, time.Now().UTC())
		return err
	})
}

func sessionCode(err error) string {
	switch {
	case errors.Is(err, accesscode.ErrTargetEpoch), errors.Is(err, accesscode.ErrInvalid), errors.Is(err, accesscode.ErrSessionScope), errors.Is(err, accesscode.ErrCodeSecret), errors.Is(err, accesscode.ErrCodeRevoked), errors.Is(err, accesscode.ErrCodeConsumed), errors.Is(err, accesscode.ErrCodeExpired):
		return protocol.Unauthorized
	default:
		return protocol.SessionUnknown
	}
}
