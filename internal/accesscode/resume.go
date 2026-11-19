package accesscode

import (
	"encoding/hex"
	"errors"
	"time"
)

// ResumeState is deliberately separate from session state. A resume
// capability is a single-use proof, not a second session authorization.
type ResumeState string

const (
	ResumeActive   ResumeState = "active"
	ResumeConsumed ResumeState = "consumed"
	ResumeRevoked  ResumeState = "revoked"
	ResumeUnknown  ResumeState = "unknown"
)

var (
	ErrResumeSecret   = errors.New("session resume capability secret is invalid")
	ErrResumeInvalid  = errors.New("session resume capability is invalid")
	ErrResumeExpired  = errors.New("session resume capability is expired")
	ErrResumeBinding  = errors.New("session resume capability binding is invalid")
	ErrResumeState    = errors.New("session resume capability state is invalid")
	ErrResumeConsumed = errors.New("session resume capability is already consumed")
)

// ResumeCapability contains only the persisted hash and trusted binding. The
// plaintext capability is never represented by this type.
type ResumeCapability struct {
	ID             string      `json:"resume_id"`
	SessionID      string      `json:"session_id"`
	BindingKind    string      `json:"binding_kind"`
	BindingDigest  string      `json:"binding_digest"`
	CapabilityHash string      `json:"capability_sha256"`
	IssuedAt       time.Time   `json:"issued_at"`
	ExpiresAt      time.Time   `json:"expires_at"`
	Generation     uint64      `json:"generation"`
	State          ResumeState `json:"state"`
}

func (r ResumeCapability) Validate() error {
	if r.ID == "" || r.SessionID == "" || r.BindingKind == "" || r.BindingDigest == "" || r.Generation == 0 || r.IssuedAt.IsZero() || r.ExpiresAt.IsZero() || !r.ExpiresAt.After(r.IssuedAt) {
		return ErrResumeInvalid
	}
	if _, err := hex.DecodeString(r.CapabilityHash); err != nil || len(r.CapabilityHash) != 64 {
		return ErrResumeInvalid
	}
	switch r.State {
	case ResumeActive, ResumeConsumed, ResumeRevoked, ResumeUnknown:
		return nil
	default:
		return ErrResumeInvalid
	}
}

// CanUse checks only the durable capability fact. The caller must separately
// verify the current session and transport authentication in the same commit.
func (r ResumeCapability) CanUse(now time.Time, sessionID, bindingKind, bindingDigest string) error {
	if err := r.Validate(); err != nil {
		return err
	}
	if r.State != ResumeActive {
		if r.State == ResumeConsumed {
			return ErrResumeConsumed
		}
		return ErrResumeState
	}
	if now.IsZero() || !now.Before(r.ExpiresAt) {
		return ErrResumeExpired
	}
	if sessionID != r.SessionID || bindingKind != r.BindingKind || bindingDigest != r.BindingDigest {
		return ErrResumeBinding
	}
	return nil
}

func (r ResumeCapability) Consume(now time.Time) (ResumeCapability, error) {
	if err := r.CanUse(now, r.SessionID, r.BindingKind, r.BindingDigest); err != nil {
		return ResumeCapability{}, err
	}
	r.State = ResumeConsumed
	return r, nil
}

func (r ResumeCapability) Revoke(now time.Time) (ResumeCapability, error) {
	if err := r.Validate(); err != nil {
		return ResumeCapability{}, err
	}
	if now.IsZero() {
		return ResumeCapability{}, ErrResumeInvalid
	}
	if r.State != ResumeActive {
		return ResumeCapability{}, ErrResumeState
	}
	r.State = ResumeRevoked
	return r, nil
}

func (r ResumeCapability) MarkUnknown(now time.Time) (ResumeCapability, error) {
	if err := r.Validate(); err != nil {
		return ResumeCapability{}, err
	}
	if now.IsZero() || r.State != ResumeActive {
		return ResumeCapability{}, ErrResumeState
	}
	r.State = ResumeUnknown
	return r, nil
}
