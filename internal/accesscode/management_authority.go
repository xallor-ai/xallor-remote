package accesscode

// ManagementIdentity is a credential identity snapshot, not a bearer token.
// These value types keep storage facts independent of access/protocol policy.
type ManagementIdentity struct {
	Authority         string
	PrincipalID       string
	TenantID          string
	MembershipID      string
	MembershipVersion int64
	CredentialID      string
	ExpiresAt         int64
}

type ManagementCredential struct {
	Identity         ManagementIdentity
	Revoked          bool
	MembershipActive bool
}

// ManagementAuthority contains only facts read in the code write transaction.
// Inbound/session epoch/share scope are not requirements for owner management.
type ManagementAuthority struct {
	DeviceUID      string
	SecretHash     string
	OwnershipID    string
	OwnerTenant    string
	OwnerPrincipal string
	Credential     ManagementCredential
}
