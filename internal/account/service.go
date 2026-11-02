// Package account owns account and membership use cases shared by local
// administration and future management transports. It does not authenticate
// websocket messages or know how the Relay stores rows.
package account

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/principal"
)

var ErrInvalidRequest = errors.New("invalid account request")

type Store interface {
	EnrollPrincipal(context.Context, string, string, string, time.Duration) (principal.Enrollment, error)
	RevokeCredential(context.Context, string) error
	CreateOrganization(context.Context, string) (string, error)
	IssueCredential(context.Context, string, time.Duration) (principal.Enrollment, error)
	SetMembershipActive(context.Context, string, bool) error
}

type Service struct{ store Store }

func New(store Store) *Service { return &Service{store: store} }

type CreatePrincipalRequest struct {
	Name, Kind, TenantID string
	TTL                  time.Duration
}

func (s *Service) CreatePrincipal(ctx context.Context, req CreatePrincipalRequest) (principal.Enrollment, error) {
	if err := validatePrincipalRequest(req); err != nil {
		return principal.Enrollment{}, err
	}
	return s.store.EnrollPrincipal(ctx, strings.TrimSpace(req.Name), req.Kind, req.TenantID, req.TTL)
}

type IssueCredentialRequest struct {
	MembershipID string
	TTL          time.Duration
}

func (s *Service) IssueCredential(ctx context.Context, req IssueCredentialRequest) (principal.Enrollment, error) {
	if !validID(req.MembershipID) || req.TTL < time.Minute || req.TTL > 90*24*time.Hour {
		return principal.Enrollment{}, ErrInvalidRequest
	}
	return s.store.IssueCredential(ctx, req.MembershipID, req.TTL)
}

func (s *Service) CreateOrganization(ctx context.Context, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 200 {
		return "", ErrInvalidRequest
	}
	return s.store.CreateOrganization(ctx, name)
}

func (s *Service) RevokeCredential(ctx context.Context, id string) error {
	if !validID(id) {
		return ErrInvalidRequest
	}
	return s.store.RevokeCredential(ctx, id)
}

func (s *Service) SetMembershipActive(ctx context.Context, id string, active bool) error {
	if !validID(id) {
		return ErrInvalidRequest
	}
	return s.store.SetMembershipActive(ctx, id, active)
}

func validatePrincipalRequest(req CreatePrincipalRequest) error {
	if principal.ValidateEnrollment(req.Name, req.Kind, req.TTL) != nil {
		return ErrInvalidRequest
	}
	if req.TenantID != "" && !validID(req.TenantID) {
		return ErrInvalidRequest
	}
	return nil
}

func validID(value string) bool {
	if len(value) != 32 {
		return false
	}
	for _, c := range value {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
