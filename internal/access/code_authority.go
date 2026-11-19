package access

import (
	"time"

	"github.com/xallor-ai/xallor-remote/internal/accesscode"
	"github.com/xallor-ai/xallor-remote/internal/principal"
)

// VerifyCodeManagementAuthority checks transactional facts against the exact
// connection/actor snapshot that entered management, never a newly adopted actor.
func VerifyCodeManagementAuthority(current accesscode.ManagementAuthority, expected Device, actor principal.Identity, now time.Time) error {
	if expected.UID == "" || expected.SecretHash == "" || expected.OwnershipID == "" || !expected.OwnedBy(actor) ||
		current.DeviceUID != expected.UID || current.SecretHash != expected.SecretHash || current.OwnershipID != expected.OwnershipID ||
		current.OwnerPrincipal != actor.PrincipalID || current.OwnerTenant != actor.TenantID {
		return ErrDenied
	}
	credential := principal.Credential{Identity: principal.Identity(current.Credential.Identity), Revoked: current.Credential.Revoked, MembershipActive: current.Credential.MembershipActive}
	if actor.Authority == "" || actor.MembershipID == "" || actor.MembershipVersion <= 0 || actor.CredentialID == "" || credential.Verify(actor, now.Unix()) != nil {
		return ErrDenied
	}
	return nil
}
