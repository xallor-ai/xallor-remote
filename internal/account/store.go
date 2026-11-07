package account

import "context"

// Store commits all callback writes atomically before returning nil. It must
// never replay the callback after an uncertain commit. The transaction cannot
// escape its callback or be shared with another goroutine.
type Store interface {
	Authority() string
	TransactAccount(context.Context, func(Transaction) error) error
}

// Transaction persists application decisions; it does not generate identities,
// choose roles, create implicit spaces, or issue plaintext credentials.
type Transaction interface {
	InsertTenant(context.Context, Tenant) error
	InsertPrincipal(context.Context, Principal) error
	InsertMembership(context.Context, Membership) error
	LoadMembership(context.Context, string) (Membership, error)
	InsertCredential(context.Context, Credential) error
	RevokeCredential(context.Context, string) error
	SetMembershipActive(context.Context, string, bool) error
}

type Tenant struct{ ID, Name, Kind string }
type Principal struct{ ID, Name, Kind string }
type Membership struct {
	ID, TenantID, PrincipalID, Role string
	Active                          bool
	Version                         int64
}
type Credential struct {
	ID, MembershipID, TokenHash string
	ExpiresAt                   int64
}
