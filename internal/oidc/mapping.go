package oidc

import (
	"context"
	"errors"
	"strings"
	"time"
)

var (
	ErrMappingNotFound = errors.New("OIDC subject mapping not found")
	ErrMappingConflict = errors.New("OIDC subject mapping version conflict")
	ErrMappingInvalid  = errors.New("invalid OIDC subject mapping")
)

// IssuerConfig is management-plane trust metadata. It is versioned so a
// rotation cannot silently overwrite a newer configuration, and expires so a
// forgotten provider fails closed even if its keys remain locally cached.
type IssuerConfig struct {
	Issuer    string `json:"issuer"`
	Audience  string `json:"audience"`
	JWKSURL   string `json:"jwks_url"`
	Version   int64  `json:"version"`
	Active    bool   `json:"active"`
	ExpiresAt int64  `json:"expires_at"`
}

func (c IssuerConfig) Verifier() Config {
	return Config{Issuer: c.Issuer, Audience: c.Audience, JWKSURL: c.JWKSURL}
}

// Mapping binds one verified external subject to one local membership. The
// subject key is issuer-scoped; provider tenant/principal claims are never
// copied into this record.
type Mapping struct {
	Subject
	MembershipRef
	Version   int64 `json:"version"`
	Active    bool  `json:"active"`
	ExpiresAt int64 `json:"expires_at"`
}

type MappingStore interface {
	PutIssuer(context.Context, IssuerConfig) error
	RevokeIssuer(context.Context, string, int64) error
	LoadIssuer(context.Context, string) (IssuerConfig, error)
	PutMapping(context.Context, Mapping) error
	RevokeMapping(context.Context, Subject, int64) error
	LookupMapping(context.Context, Subject) (Mapping, error)
}

// MappingService is the provider-neutral management adapter between verified
// issuer subjects and local account membership. Its store must validate the
// membership row and version in the same read used by LookupMapping.
type MappingService struct {
	store MappingStore
	now   func() time.Time
}

func NewMappingService(store MappingStore) *MappingService {
	return &MappingService{store: store, now: time.Now}
}

func (s *MappingService) ConfigureIssuer(ctx context.Context, cfg IssuerConfig) error {
	if s == nil || s.store == nil {
		return ErrMappingInvalid
	}
	if err := validateIssuerConfig(cfg, s.now()); err != nil {
		return err
	}
	return s.store.PutIssuer(ctx, cfg)
}

func (s *MappingService) RevokeIssuer(ctx context.Context, issuer string, version int64) error {
	if s == nil || s.store == nil || strings.TrimSpace(issuer) == "" || version <= 0 {
		return ErrMappingInvalid
	}
	return s.store.RevokeIssuer(ctx, issuer, version)
}

// Map creates or rotates a subject binding. Existing bindings require a
// strictly newer version, which prevents stale administration writes from
// restoring a revoked or superseded identity.
func (s *MappingService) Map(ctx context.Context, mapping Mapping) error {
	if s == nil || s.store == nil || !validMapping(mapping) {
		return ErrMappingInvalid
	}
	cfg, err := s.store.LoadIssuer(ctx, mapping.Issuer)
	if err != nil {
		return err
	}
	if err := validateIssuerConfig(cfg, s.now()); err != nil {
		return ErrMappingNotFound
	}
	if mapping.ExpiresAt <= s.now().Unix() {
		return ErrMappingInvalid
	}
	return s.store.PutMapping(ctx, mapping)
}

func (s *MappingService) Revoke(ctx context.Context, subject Subject, version int64) error {
	if s == nil || s.store == nil || strings.TrimSpace(subject.Issuer) == "" || strings.TrimSpace(subject.Value) == "" || version <= 0 {
		return ErrMappingInvalid
	}
	return s.store.RevokeMapping(ctx, subject, version)
}

// Lookup implements SubjectMapper. Every lookup rechecks issuer and mapping
// active/expiry state; the store additionally matches the current membership
// tenant, principal and version before returning a row.
func (s *MappingService) Lookup(ctx context.Context, subject Subject) (MembershipRef, error) {
	if s == nil || s.store == nil || strings.TrimSpace(subject.Issuer) == "" || strings.TrimSpace(subject.Value) == "" {
		return MembershipRef{}, ErrMappingNotFound
	}
	cfg, err := s.store.LoadIssuer(ctx, subject.Issuer)
	if err != nil || validateIssuerConfig(cfg, s.now()) != nil {
		return MembershipRef{}, ErrMappingNotFound
	}
	mapping, err := s.store.LookupMapping(ctx, subject)
	if err != nil || !mapping.Active || mapping.Version <= 0 || mapping.ExpiresAt <= s.now().Unix() {
		return MembershipRef{}, ErrMappingNotFound
	}
	return mapping.MembershipRef, nil
}

func validateIssuerConfig(c IssuerConfig, now time.Time) error {
	if c.Verifier().Validate() != nil || c.Version <= 0 || !c.Active || c.ExpiresAt <= now.Unix() {
		return ErrMappingInvalid
	}
	return nil
}

func validMapping(m Mapping) bool {
	return strings.TrimSpace(m.Issuer) != "" && strings.TrimSpace(m.Value) != "" &&
		validLocalID(m.TenantID) && validLocalID(m.PrincipalID) && validLocalID(m.MembershipID) &&
		m.MembershipVersion > 0 && m.Version > 0 && m.Active && m.ExpiresAt > 0
}

func validLocalID(value string) bool {
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
