package relay

import (
	"context"
	"errors"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/accesscode"
	"github.com/xallor-ai/xallor-remote/internal/principal"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Revalidate the original device/actor snapshot inside every transaction
// attempt, including repeat rotation intents. No successful response precedes
// commit. The result contains metadata and a private intent binding, not a secret.
func (h *Hub) manageTypedCode(ctx context.Context, deviceID string, device access.Device, actor principal.Identity, req access.Request) (any, string, uint64) {
	var generation uint64
	var rotated accesscode.Code
	var intent string
	if req.Action == "code.rotate" {
		var err error
		intent, err = accesscode.RotationDigest(req.CodeID, req.NewCodeID, protocol.SHA256Hex(req.NewSecret))
		if err != nil {
			return nil, protocol.AgentError, 0
		}
	}
	err := h.accessCodes.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		rotated = accesscode.Code{}
		code, err := tx.Code(ctx, req.CodeID)
		if err != nil {
			return err
		}
		if code.DeviceUID != device.UID || code.OwnershipID != device.OwnershipID {
			return accesscode.ErrCodeSecret
		}
		current, err := tx.CodeManagementAuthority(ctx, deviceID, actor.CredentialID)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		now := time.Now().UTC()
		if err := access.VerifyCodeManagementAuthority(current, device, actor, now); err != nil {
			return err
		}
		generation = code.Generation
		if req.Action == "code.revoke" {
			_, err = tx.RevokeCode(ctx, req.CodeID)
			return err
		}
		rotated, err = tx.RotateCode(ctx, req.CodeID, req.NewCodeID, protocol.SHA256Hex(req.NewSecret), now)
		return err
	})
	if err != nil {
		return nil, accessCodeManagementError(err), 0
	}
	if req.Action == "code.revoke" {
		return access.CodeRevocationResult{CodeID: req.CodeID, State: accesscode.StateRevoked}, "", generation
	}
	return access.CodeRotationReply{CodeRotationResult: access.CodeRotationResult{PreviousCodeID: req.CodeID, Code: rotated}, IntentSHA256: intent}, "", generation
}

func accessCodeManagementError(err error) string {
	switch {
	case errors.Is(err, access.ErrDenied), errors.Is(err, access.ErrDeviceNotFound), errors.Is(err, principal.ErrUnauthorized),
		errors.Is(err, accesscode.ErrCodeSecret), errors.Is(err, accesscode.ErrCodeRevoked), errors.Is(err, accesscode.ErrCodeConsumed), errors.Is(err, accesscode.ErrCodeExpired), errors.Is(err, accesscode.ErrRotationConflict), errors.Is(err, accesscode.ErrReservationState):
		return protocol.Unauthorized
	case errors.Is(err, accesscode.ErrInvalid):
		return protocol.AgentError
	default:
		return protocol.RelayError
	}
}
