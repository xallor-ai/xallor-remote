package principal

import (
	"context"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Credential is one consistent authority/membership/credential snapshot. The
// adapter returns revoked and inactive records too; policy belongs here.
type Credential struct {
	Identity
	Revoked          bool
	MembershipActive bool
}

func (c Credential) Verify(expected Identity, now int64) error {
	if !c.ValidAt(now) || c.Identity != expected {
		return ErrUnauthorized
	}
	return nil
}

func (c Credential) ValidAt(now int64) bool {
	return c.MembershipActive && !c.Revoked && c.ExpiresAt > now
}

// Reader must resolve each credential and its membership in one snapshot.
// Missing records return ErrUnauthorized; storage failures remain distinct.
type Reader interface {
	CredentialByID(context.Context, string) (Credential, error)
	CredentialByTokenHash(context.Context, string) (Credential, error)
}

// Service owns authentication and connection revalidation. Time is evaluated
// after storage returns so a pool/lock wait cannot revive an expired credential.
type Service struct {
	reader Reader
	now    func() time.Time
}

func NewService(reader Reader) *Service { return &Service{reader: reader, now: time.Now} }

var _ Authenticator = (*Service)(nil)

func (s *Service) Authenticate(ctx context.Context, token string) (Identity, error) {
	if !ValidToken(token) {
		return Identity{}, ErrUnauthorized
	}
	credential, err := s.reader.CredentialByTokenHash(ctx, protocol.SHA256Hex(token))
	if err != nil {
		return Identity{}, err
	}
	if err := ctx.Err(); err != nil {
		return Identity{}, err
	}
	if !credential.ValidAt(s.now().Unix()) {
		return Identity{}, ErrUnauthorized
	}
	return credential.Identity, nil
}

func (s *Service) Verify(ctx context.Context, expected Identity) error {
	credential, err := s.reader.CredentialByID(ctx, expected.CredentialID)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return credential.Verify(expected, s.now().Unix())
}
