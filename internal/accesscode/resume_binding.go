package accesscode

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// These value types keep the resume digest independent of transport packages.
// Both connection snapshots and transactional authority reads use this shape.
type ResumeAccessBinding struct{ DeviceUID, GrantID, OwnershipID string }

type ResumeIdentity struct {
	Authority         string `json:"authority"`
	PrincipalID       string `json:"principal_id"`
	TenantID          string `json:"tenant_id"`
	MembershipID      string `json:"membership_id"`
	MembershipVersion int64  `json:"membership_version"`
	CredentialID      string `json:"credential_id"`
	ExpiresAt         int64  `json:"expires_at"`
}

type ResumeAuthorization struct {
	Binding     ResumeAccessBinding
	ShareID     string
	Permissions uint64
	ExpiresAt   int64
}

type ResumeBinding struct {
	TargetID      string
	Access        ResumeAccessBinding
	Identity      ResumeIdentity
	Authorization ResumeAuthorization
}

// Digest binds the snapshot only. Current credential/share validity must be
// checked against authoritative state by the transaction that consumes proof.
func (b ResumeBinding) Digest() (string, string) {
	if b.TargetID == "" || b.Access.DeviceUID == "" || (b.Access.GrantID == "") == (b.Access.OwnershipID == "") || b.Authorization.Binding != b.Access || b.Authorization.Permissions == 0 {
		return "", ""
	}
	kind := "grant"
	actor := b.Identity
	if actor != (ResumeIdentity{}) {
		if actor.Authority == "" || actor.PrincipalID == "" || actor.TenantID == "" || actor.MembershipID == "" || actor.MembershipVersion <= 0 || actor.CredentialID == "" || actor.ExpiresAt <= 0 {
			return "", ""
		}
		kind = "account"
	} else if b.Access.OwnershipID != "" {
		return "", ""
	}
	facts := struct {
		Version       int                 `json:"version"`
		Kind          string              `json:"kind"`
		TargetID      string              `json:"target_id"`
		Access        ResumeAccessBinding `json:"access"`
		Identity      ResumeIdentity      `json:"identity"`
		Authorization ResumeAuthorization `json:"authorization"`
	}{1, kind, b.TargetID, b.Access, actor, b.Authorization}
	raw, err := json.Marshal(facts)
	if err != nil {
		return "", ""
	}
	sum := sha256.Sum256(raw)
	return kind, hex.EncodeToString(sum[:])
}
