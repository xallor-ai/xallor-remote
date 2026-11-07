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
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

var ErrInvalidRequest = errors.New("invalid account request")

type Service struct {
	store Store
	now   func() time.Time
}

func New(store Store) *Service { return &Service{store: store, now: time.Now} }

type CreatePrincipalRequest struct {
	Name, Kind, TenantID string
	TTL                  time.Duration
}

func (s *Service) CreatePrincipal(ctx context.Context, req CreatePrincipalRequest) (principal.Enrollment, error) {
	if err := validatePrincipalRequest(req); err != nil {
		return principal.Enrollment{}, err
	}
	var result principal.Enrollment
	err := s.store.TransactAccount(ctx, func(tx Transaction) error {
		var ids [3]string
		for i := range ids {
			id, err := protocol.RandomHex(16)
			if err != nil {
				return err
			}
			ids[i] = id
		}
		name := strings.TrimSpace(req.Name)
		member := Membership{ID: ids[2], PrincipalID: ids[0], TenantID: req.TenantID, Role: "member", Active: true, Version: 1}
		if member.TenantID == "" {
			member.TenantID, member.Role = ids[1], "owner"
			if err := tx.InsertTenant(ctx, Tenant{ID: member.TenantID, Name: name, Kind: "personal"}); err != nil {
				return err
			}
		}
		if err := tx.InsertPrincipal(ctx, Principal{ID: member.PrincipalID, Name: name, Kind: req.Kind}); err != nil {
			return err
		}
		if err := tx.InsertMembership(ctx, member); err != nil {
			return err
		}
		var err error
		result, err = s.issue(ctx, tx, member, req.TTL)
		return err
	})
	if err != nil {
		return principal.Enrollment{}, err
	}
	return result, nil
}

type IssueCredentialRequest struct {
	MembershipID string
	TTL          time.Duration
}

func (s *Service) IssueCredential(ctx context.Context, req IssueCredentialRequest) (principal.Enrollment, error) {
	if !validID(req.MembershipID) || req.TTL < time.Minute || req.TTL > 90*24*time.Hour {
		return principal.Enrollment{}, ErrInvalidRequest
	}
	var result principal.Enrollment
	err := s.store.TransactAccount(ctx, func(tx Transaction) error {
		member, err := tx.LoadMembership(ctx, req.MembershipID)
		if err != nil {
			return err
		}
		result, err = s.issue(ctx, tx, member, req.TTL)
		return err
	})
	if err != nil {
		return principal.Enrollment{}, err
	}
	return result, nil
}

func (s *Service) issue(ctx context.Context, tx Transaction, member Membership, ttl time.Duration) (principal.Enrollment, error) {
	if !member.Active {
		return principal.Enrollment{}, principal.ErrUnauthorized
	}
	id, err := protocol.RandomHex(16)
	if err != nil {
		return principal.Enrollment{}, err
	}
	token, err := principal.NewToken()
	if err != nil {
		return principal.Enrollment{}, err
	}
	expires := s.now().Add(ttl).Unix()
	if err := tx.InsertCredential(ctx, Credential{ID: id, MembershipID: member.ID, TokenHash: protocol.SHA256Hex(token), ExpiresAt: expires}); err != nil {
		return principal.Enrollment{}, err
	}
	return principal.Enrollment{Token: token, Identity: principal.Identity{
		Authority: s.store.Authority(), PrincipalID: member.PrincipalID,
		TenantID: member.TenantID, MembershipID: member.ID, MembershipVersion: member.Version,
		CredentialID: id, ExpiresAt: expires,
	}}, nil
}

func (s *Service) CreateOrganization(ctx context.Context, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 200 {
		return "", ErrInvalidRequest
	}
	id, err := protocol.RandomHex(16)
	if err != nil {
		return "", err
	}
	err = s.store.TransactAccount(ctx, func(tx Transaction) error {
		return tx.InsertTenant(ctx, Tenant{ID: id, Name: name, Kind: "organization"})
	})
	if err != nil {
		return "", err
	}
	return id, nil
}

func (s *Service) RevokeCredential(ctx context.Context, id string) error {
	if !validID(id) {
		return ErrInvalidRequest
	}
	return s.store.TransactAccount(ctx, func(tx Transaction) error { return tx.RevokeCredential(ctx, id) })
}

func (s *Service) SetMembershipActive(ctx context.Context, id string, active bool) error {
	if !validID(id) {
		return ErrInvalidRequest
	}
	return s.store.TransactAccount(ctx, func(tx Transaction) error { return tx.SetMembershipActive(ctx, id, active) })
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
