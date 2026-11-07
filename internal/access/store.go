package access

import (
	"context"
	"github.com/xallor-ai/xallor-remote/internal/principal"
)

// Store provides a consistent snapshot per callback. WriteAccess commits all
// writes before success; only a confirmed rollback permits a retry. Callbacks
// may not publish effects outside the transaction or retain the transaction.
type Store interface {
	ReadAccess(context.Context, func(Reader) error) error
	WriteAccess(context.Context, func(Transaction) error) error
}

type Reader interface {
	// CredentialByID returns membership/credential state from this transaction,
	// without deciding validity. Missing records return principal.ErrUnauthorized.
	CredentialByID(context.Context, string) (principal.Credential, error)
	DeviceByID(context.Context, string) (Device, error)
	// Missing UID/member/share returns ErrDenied, never an empty success.
	DeviceByUID(context.Context, string) (Device, error)
	Member(context.Context, string) (Member, error)
	Share(context.Context, string, string) (ShareRecord, error)
	Shares(context.Context, string) ([]ShareRecord, error)
}

type Transaction interface {
	Reader
	ReplaceOwnership(context.Context, Ownership) error
	DeleteMemberShare(context.Context, string, string) error
	DeleteExpiredShares(context.Context, string, int64) error
	CountShares(context.Context, string) (int, error)
	InsertShare(context.Context, ShareRecord) error
	DeleteShare(context.Context, string, string) error
}

type Member struct {
	TenantID, PrincipalID string
	Active                bool
}

type Ownership struct{ ID, DeviceUID, TenantID, PrincipalID string }

type ShareRecord struct {
	ID, OwnershipID, MembershipID string
	Permissions                   Permissions
	ExpiresAt                     int64
}
