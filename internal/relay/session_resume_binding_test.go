package relay

import (
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/principal"
)

// A saved resume proof must not outlive the identity/access incarnation it
// was issued for, including restoring equal permissions under a new share ID.
func TestResumeBinding_shouldChange_whenAuthenticatedContextChanges(t *testing.T) {
	base := clientSlot{targetID: "device", access: access.Binding{DeviceUID: "uid", OwnershipID: "owner"}, identity: principal.Identity{Authority: "authority", PrincipalID: "alice", TenantID: "tenant", MembershipID: "member", MembershipVersion: 1, CredentialID: "credential", ExpiresAt: 1000}}
	base.authorization = access.Authorization{Binding: base.access, ShareID: "share", Permissions: access.AllPermissions(), ExpiresAt: 900}
	kind, digest := sessionResumeBinding(base, true)
	if kind == "" || digest == "" {
		t.Fatal("valid authenticated context refused")
	}
	for name, change := range map[string]func(*clientSlot){
		"target":             func(s *clientSlot) { s.targetID = "other" },
		"device-uid":         func(s *clientSlot) { s.access.DeviceUID = "other"; s.authorization.Binding = s.access },
		"authority":          func(s *clientSlot) { s.identity.Authority = "other" },
		"principal":          func(s *clientSlot) { s.identity.PrincipalID = "bob" },
		"tenant":             func(s *clientSlot) { s.identity.TenantID = "other" },
		"membership":         func(s *clientSlot) { s.identity.MembershipID = "other" },
		"membership-version": func(s *clientSlot) { s.identity.MembershipVersion++ },
		"credential":         func(s *clientSlot) { s.identity.CredentialID = "other" },
		"credential-expiry":  func(s *clientSlot) { s.identity.ExpiresAt++ },
		"share":              func(s *clientSlot) { s.authorization.ShareID = "replacement" },
		"permissions":        func(s *clientSlot) { s.authorization.Permissions = 1 },
		"share-expiry":       func(s *clientSlot) { s.authorization.ExpiresAt++ },
	} {
		t.Run(name, func(t *testing.T) {
			changed := base
			change(&changed)
			otherKind, otherDigest := sessionResumeBinding(changed, true)
			if otherKind == kind && otherDigest == digest {
				t.Fatal("changed context retained resume binding")
			}
		})
	}
}

// An ownership row alone does not identify the caller. Missing identity or a
// mismatched authorization snapshot must not produce a usable resume binding.
func TestResumeBinding_shouldRefuseIncompleteOwnedContext(t *testing.T) {
	slot := clientSlot{targetID: "device", access: access.Binding{DeviceUID: "uid", OwnershipID: "owner"}}
	if kind, digest := sessionResumeBinding(slot, true); kind != "" || digest != "" {
		t.Fatal("anonymous ownership binding accepted")
	}
}
