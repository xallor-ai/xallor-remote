package accesscode

import (
	"context"
	"time"
)

// Store provides one transaction boundary for code redemption and session
// authorization. Implementations must not publish success before commit.
type Store interface {
	ReadAccessCode(context.Context, func(Reader) error) error
	WriteAccessCode(context.Context, func(Transaction) error) error
}

type Reader interface {
	Code(context.Context, string) (Code, error)
	Reservation(context.Context, string) (Reservation, error)
	Session(context.Context, string) (SessionAuthorization, error)
	Resume(context.Context, string) (ResumeCapability, error)
	ActiveResumeForSession(context.Context, string) (ResumeCapability, error)
	Rotation(context.Context, string) (Rotation, error)
}

type Transaction interface {
	Reader
	// CodeManagementAuthority reads current device and credential/member facts
	// in this transaction; it must not substitute a cached authentication.
	CodeManagementAuthority(context.Context, string, string) (ManagementAuthority, error)
	InsertCode(context.Context, Code, string) error
	ActivateSessionTarget(context.Context, TargetBinding, time.Time) error
	RetireSessionTarget(context.Context, TargetBinding, time.Time) error
	RequireSessionTarget(context.Context, TargetBinding) error
	ReserveCode(context.Context, string, string, string, string, time.Time, time.Time, TargetBinding) (Code, Reservation, error)
	FinalizeReservation(context.Context, string, bool, time.Time, *time.Time, []string) (Code, Reservation, SessionAuthorization, error)
	MarkReservationUnknown(context.Context, string, time.Time) (Reservation, error)
	RevokeCode(context.Context, string) (Code, error)
	RotateCode(context.Context, string, string, string, time.Time) (Code, error)
	EndSession(context.Context, string, time.Time) (SessionAuthorization, error)
	RevokeSession(context.Context, string, time.Time) (SessionAuthorization, error)
	InsertResume(context.Context, ResumeCapability) error
	// ResumeAuthority reads current device, account and share facts in this transaction.
	ResumeAuthority(context.Context, string, string) (ResumeBinding, error)
	// ConsumeResume authenticates the caller-supplied SHA-256 before changing state.
	ConsumeResume(context.Context, string, string, time.Time, string, string, string) (ResumeCapability, error)
	RevokeResume(context.Context, string, time.Time) (ResumeCapability, error)
	MarkResumeUnknown(context.Context, string, time.Time) (ResumeCapability, error)
}
