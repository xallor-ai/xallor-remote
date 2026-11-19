package access

import (
	"errors"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/accesscode"
	"github.com/xallor-ai/xallor-remote/internal/principal"
)

// A still-live credential is insufficient if any identity field differs from
// the authenticated request snapshot; in particular renewal cannot silently
// replace membership, credential or expiry facts mid-management operation.
func TestCodeAuthority_shouldRequireExactIdentity_andCurrentOwner(t *testing.T) {
	actor := principal.Identity{Authority: "authority", PrincipalID: "owner", TenantID: "tenant", MembershipID: "member", MembershipVersion: 1, CredentialID: "credential", ExpiresAt: 200}
	expected := Device{UID: "device", SecretHash: "device-hash", OwnershipID: "ownership", OwnerPrincipal: "owner", OwnerTenant: "tenant"}
	base := accesscode.ManagementAuthority{DeviceUID: "device", SecretHash: "device-hash", OwnershipID: "ownership", OwnerPrincipal: "owner", OwnerTenant: "tenant", Credential: accesscode.ManagementCredential{Identity: accesscode.ManagementIdentity(actor), MembershipActive: true}}
	if err := VerifyCodeManagementAuthority(base, expected, actor, time.Unix(100, 0)); err != nil {
		t.Fatal("valid owner refused", err)
	}
	for _, tc := range []struct {
		name   string
		change func(*accesscode.ManagementAuthority)
	}{
		{"authority", func(a *accesscode.ManagementAuthority) { a.Credential.Identity.Authority = "different" }},
		{"principal", func(a *accesscode.ManagementAuthority) { a.Credential.Identity.PrincipalID = "different" }},
		{"tenant", func(a *accesscode.ManagementAuthority) { a.Credential.Identity.TenantID = "different" }},
		{"member", func(a *accesscode.ManagementAuthority) { a.Credential.Identity.MembershipID = "different" }},
		{"version", func(a *accesscode.ManagementAuthority) { a.Credential.Identity.MembershipVersion++ }},
		{"credential", func(a *accesscode.ManagementAuthority) { a.Credential.Identity.CredentialID = "different" }},
		{"extended-expiry", func(a *accesscode.ManagementAuthority) { a.Credential.Identity.ExpiresAt++ }},
		{"revoked", func(a *accesscode.ManagementAuthority) { a.Credential.Revoked = true }},
		{"inactive", func(a *accesscode.ManagementAuthority) { a.Credential.MembershipActive = false }},
		{"owner-principal", func(a *accesscode.ManagementAuthority) { a.OwnerPrincipal = "different" }},
		{"owner-tenant", func(a *accesscode.ManagementAuthority) { a.OwnerTenant = "different" }},
		{"device", func(a *accesscode.ManagementAuthority) { a.DeviceUID = "different" }},
		{"device-proof", func(a *accesscode.ManagementAuthority) { a.SecretHash = "different" }},
		{"ownership", func(a *accesscode.ManagementAuthority) { a.OwnershipID = "different" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := base
			tc.change(&changed)
			if !errors.Is(VerifyCodeManagementAuthority(changed, expected, actor, time.Unix(100, 0)), ErrDenied) {
				t.Fatal("changed authority accepted")
			}
		})
	}
	if !errors.Is(VerifyCodeManagementAuthority(base, expected, actor, time.Unix(200, 0)), ErrDenied) {
		t.Fatal("exact expiry accepted")
	}
}
