package relay

import (
	"context"
	"errors"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/accesscode"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

func (h *Hub) handleSessionResumeRequest(from Sender, targetID string, msg protocol.Message) {
	reply := func(status, code string, generation uint64, scope []string, expires *int64, resumeID, capability string) {
		_ = from.Enqueue(protocol.Message{Type: protocol.TypeSessionResumeResult, RequestID: msg.RequestID, SessionID: msg.SessionID, Status: status, Code: code, CodeGeneration: generation, SessionScope: scope, SessionExpiresAt: expires, ResumeID: resumeID, ResumeCapability: capability})
	}
	if h.accessCodes == nil || protocol.ValidateSessionResumeRequest(msg) != nil {
		reply(protocol.SessionRejectedState, protocol.Unauthorized, 0, nil, nil, "", "")
		return
	}
	h.mu.Lock()
	client, clientOK := h.clients[from]
	device := h.devices[targetID]
	var deviceConnection Sender
	var expectedTarget accesscode.TargetBinding
	if device != nil {
		deviceConnection = device.conn
		expectedTarget = device.sessionTarget
	}
	bindingKind, bindingDigest := sessionResumeBinding(client, clientOK)
	ready := device != nil && device.online && device.ready && device.record.UID == msg.TargetUID
	h.mu.Unlock()
	if !clientOK || client.targetID != targetID || bindingKind == "" || !ready {
		reply(protocol.SessionRejectedState, protocol.Unauthorized, 0, nil, nil, "", "")
		return
	}

	ctx, cancel := context.WithTimeout(from.Context(), protocol.RelayIdentityTimeout)
	defer cancel()
	var session accesscode.SessionAuthorization
	var rotated *accesscode.ResumeCapability
	var rotatedSecret string
	err := h.accessCodes.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		session, rotated, rotatedSecret = accesscode.SessionAuthorization{}, nil, ""
		authority, err := tx.ResumeAuthority(ctx, targetID, client.identity.CredentialID)
		if err != nil {
			return err
		}
		currentKind, currentDigest := authority.Digest()
		if currentKind == "" || currentKind != bindingKind || currentDigest != bindingDigest || authority.Access.DeviceUID != msg.TargetUID {
			return accesscode.ErrResumeBinding
		}
		current, err := tx.Session(ctx, msg.SessionID)
		if err != nil {
			return err
		}
		if current.Target != expectedTarget || !expectedTarget.Valid() {
			return accesscode.ErrTargetEpoch
		}
		now := time.Now().UTC()
		// SQL reads may wait across credential/share expiry. Recheck their
		// deadlines immediately before consuming the capability.
		if (authority.Identity.CredentialID != "" && authority.Identity.ExpiresAt <= now.Unix()) || (authority.Authorization.ExpiresAt != 0 && authority.Authorization.ExpiresAt <= now.Unix()) {
			return accesscode.ErrResumeBinding
		}
		if err := current.CanOperate(now); err != nil {
			return err
		}
		_, err = tx.ConsumeResume(ctx, msg.ResumeID, protocol.SHA256Hex(msg.ResumeCapability), now, msg.SessionID, bindingKind, bindingDigest)
		if err != nil {
			return err
		}
		secret, err := protocol.RandomHex(32)
		if err != nil {
			return err
		}
		resumeID, err := protocol.RandomHex(16)
		if err != nil {
			return err
		}
		expires := now.Add(sessionResumeTTL)
		if current.ExpiresAt != nil && current.ExpiresAt.Before(expires) {
			expires = *current.ExpiresAt
		}
		candidate := accesscode.ResumeCapability{ID: resumeID, SessionID: current.ID, BindingKind: bindingKind, BindingDigest: bindingDigest, CapabilityHash: protocol.SHA256Hex(secret), IssuedAt: now, ExpiresAt: expires, Generation: current.Generation, State: accesscode.ResumeActive}
		if err := tx.InsertResume(ctx, candidate); err != nil {
			return err
		}
		session, rotated, rotatedSecret = current, &candidate, secret
		return nil
	})
	if err != nil {
		status, code := resumeError(err)
		reply(status, code, 0, nil, nil, "", "")
		return
	}
	result := protocol.Message{Type: protocol.TypeSessionResumeResult, RequestID: msg.RequestID, SessionID: msg.SessionID, Status: protocol.SessionActive, CodeGeneration: session.Generation, SessionScope: append([]string(nil), session.Scope...), SessionExpiresAt: sessionExpiryUnix(session.ExpiresAt), ResumeID: rotated.ID, ResumeCapability: rotatedSecret}
	if !h.publishResumedSession(ctx, from, client, device, deviceConnection, &session, rotated, result) {
		_ = h.retireUnpublishedSession(session.ID)
		reply(protocol.SessionUnknownState, protocol.SessionUnknown, 0, nil, nil, "", "")
		from.Close()
	}
}

func resumeError(err error) (string, string) {
	switch {
	case errors.Is(err, accesscode.ErrResumeExpired), errors.Is(err, accesscode.ErrSessionExpired):
		return protocol.SessionExpiredState, protocol.SessionExpiredState
	case errors.Is(err, accesscode.ErrTargetEpoch), errors.Is(err, accesscode.ErrResumeSecret), errors.Is(err, accesscode.ErrResumeConsumed), errors.Is(err, accesscode.ErrResumeState), errors.Is(err, accesscode.ErrResumeBinding), errors.Is(err, accesscode.ErrResumeInvalid), errors.Is(err, accesscode.ErrSessionState):
		return protocol.SessionRejectedState, protocol.Unauthorized
	default:
		return protocol.SessionUnknownState, protocol.SessionUnknown
	}
}
