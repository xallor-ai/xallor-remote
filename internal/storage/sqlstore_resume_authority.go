package storage

import (
	"context"
	"errors"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/accesscode"
	"github.com/xallor-ai/xallor-remote/internal/principal"
)

func (t accessCodeTransaction) ResumeAuthority(ctx context.Context, targetID, credentialID string) (accesscode.ResumeBinding, error) {
	reader := accessTransaction{tx: t.tx}
	device, err := reader.DeviceByID(ctx, targetID)
	if err != nil {
		return accesscode.ResumeBinding{}, resumeAuthorityError(err)
	}
	if !device.Inbound {
		return accesscode.ResumeBinding{}, accesscode.ErrResumeBinding
	}
	var actor principal.Identity
	var credential principal.Credential
	if credentialID != "" {
		credential, err = reader.CredentialByID(ctx, credentialID)
		if err != nil {
			return accesscode.ResumeBinding{}, resumeAuthorityError(err)
		}
		if !credential.ValidAt(time.Now().Unix()) {
			return accesscode.ResumeBinding{}, accesscode.ErrResumeBinding
		}
		actor = credential.Identity
	}
	authorization := access.Authorization{Binding: device.Binding(), Permissions: access.AllPermissions()}
	if device.OwnershipID != "" {
		authorization, err = access.ResolveIn(ctx, reader, device, actor)
		if err != nil {
			return accesscode.ResumeBinding{}, resumeAuthorityError(err)
		}
	}
	// Evaluate time again after all SQL reads, never using a pre-pool timestamp.
	if credentialID != "" && !credential.ValidAt(time.Now().Unix()) {
		return accesscode.ResumeBinding{}, accesscode.ErrResumeBinding
	}
	if authorization.ExpiresAt != 0 && authorization.ExpiresAt <= time.Now().Unix() {
		return accesscode.ResumeBinding{}, accesscode.ErrResumeBinding
	}
	return accesscode.ResumeBinding{
		TargetID: targetID, Access: accesscode.ResumeAccessBinding(device.Binding()), Identity: accesscode.ResumeIdentity(actor),
		Authorization: accesscode.ResumeAuthorization{Binding: accesscode.ResumeAccessBinding(authorization.Binding), ShareID: authorization.ShareID, Permissions: uint64(authorization.Permissions), ExpiresAt: authorization.ExpiresAt},
	}, nil
}

func resumeAuthorityError(err error) error {
	if errors.Is(err, access.ErrDenied) || errors.Is(err, access.ErrDeviceNotFound) || errors.Is(err, principal.ErrUnauthorized) {
		return accesscode.ErrResumeBinding
	}
	return err
}
