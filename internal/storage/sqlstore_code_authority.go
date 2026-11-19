package storage

import (
	"context"

	"github.com/xallor-ai/xallor-remote/internal/accesscode"
)

// Reuse the existing transactional readers; no nested transaction or cached
// owner/credential state. Missing rows retain their specific denied errors,
// while genuine storage failures remain distinguishable by the caller.
func (t accessCodeTransaction) CodeManagementAuthority(ctx context.Context, deviceID, credentialID string) (accesscode.ManagementAuthority, error) {
	reader := accessTransaction{tx: t.tx}
	device, err := reader.DeviceByID(ctx, deviceID)
	if err != nil {
		return accesscode.ManagementAuthority{}, err
	}
	credential, err := reader.CredentialByID(ctx, credentialID)
	if err != nil {
		return accesscode.ManagementAuthority{}, err
	}
	return accesscode.ManagementAuthority{
		DeviceUID: device.UID, SecretHash: device.SecretHash, OwnershipID: device.OwnershipID,
		OwnerPrincipal: device.OwnerPrincipal, OwnerTenant: device.OwnerTenant,
		Credential: accesscode.ManagementCredential{Identity: accesscode.ManagementIdentity(credential.Identity), Revoked: credential.Revoked, MembershipActive: credential.MembershipActive},
	}, nil
}
