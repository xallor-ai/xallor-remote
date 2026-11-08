package oidc

import (
	"context"
	"errors"
	"testing"
	"time"
)

type mappingMember struct {
	MembershipRef
	active bool
}

// The fake implements the persistence contract, including unique subject keys,
// monotonic writes and a consistent current membership read. No SQLite or
// network dependencies are needed by the service tests.
type mappingFake struct {
	issuers  map[string]IssuerConfig
	mappings map[Subject]Mapping
	members  map[string]mappingMember
	fail     string
	err      error
}

func (f *mappingFake) failure(operation string) error {
	if f.fail == operation {
		return f.err
	}
	return nil
}

func (f *mappingFake) PutIssuer(_ context.Context, cfg IssuerConfig) error {
	if err := f.failure("put issuer"); err != nil {
		return err
	}
	if old, ok := f.issuers[cfg.Issuer]; ok && cfg.Version <= old.Version {
		return ErrMappingConflict
	}
	f.issuers[cfg.Issuer] = cfg
	return nil
}

func (f *mappingFake) RevokeIssuer(_ context.Context, issuer string, version int64) error {
	if err := f.failure("revoke issuer"); err != nil {
		return err
	}
	cfg, ok := f.issuers[issuer]
	if !ok {
		return ErrMappingNotFound
	}
	if version <= cfg.Version {
		return ErrMappingConflict
	}
	cfg.Version, cfg.Active = version, false
	f.issuers[issuer] = cfg
	return nil
}

func (f *mappingFake) LoadIssuer(_ context.Context, issuer string) (IssuerConfig, error) {
	if err := f.failure("load issuer"); err != nil {
		return IssuerConfig{}, err
	}
	cfg, ok := f.issuers[issuer]
	if !ok {
		return IssuerConfig{}, ErrMappingNotFound
	}
	return cfg, nil
}

func (f *mappingFake) currentMember(mapping Mapping) bool {
	member, ok := f.members[mapping.MembershipID]
	return ok && member.active && member.MembershipRef == mapping.MembershipRef
}

func (f *mappingFake) PutMapping(_ context.Context, mapping Mapping) error {
	if err := f.failure("put mapping"); err != nil {
		return err
	}
	if !f.currentMember(mapping) {
		return ErrMappingNotFound
	}
	if old, ok := f.mappings[mapping.Subject]; ok && mapping.Version <= old.Version {
		return ErrMappingConflict
	}
	f.mappings[mapping.Subject] = mapping
	return nil
}

func (f *mappingFake) RevokeMapping(_ context.Context, subject Subject, version int64) error {
	if err := f.failure("revoke mapping"); err != nil {
		return err
	}
	mapping, ok := f.mappings[subject]
	if !ok {
		return ErrMappingNotFound
	}
	if version <= mapping.Version {
		return ErrMappingConflict
	}
	mapping.Active, mapping.Version = false, version
	f.mappings[subject] = mapping
	return nil
}

func (f *mappingFake) LookupMapping(_ context.Context, subject Subject) (Mapping, error) {
	if err := f.failure("lookup mapping"); err != nil {
		return Mapping{}, err
	}
	mapping, ok := f.mappings[subject]
	if !ok || !f.currentMember(mapping) {
		return Mapping{}, ErrMappingNotFound
	}
	return mapping, nil
}

func mappingFixture(t *testing.T) (*MappingService, *mappingFake, IssuerConfig, Mapping) {
	t.Helper()
	now := time.Unix(20_000, 0)
	fake := &mappingFake{issuers: map[string]IssuerConfig{}, mappings: map[Subject]Mapping{}, members: map[string]mappingMember{}, err: errors.New("store unavailable")}
	service := NewMappingService(fake)
	service.now = func() time.Time { return now }
	issuer := IssuerConfig{Issuer: "https://issuer.example", Audience: "xallor", JWKSURL: "https://issuer.example/keys", Version: 1, Active: true, ExpiresAt: now.Add(time.Hour).Unix()}
	ref := MembershipRef{TenantID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", PrincipalID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", MembershipID: "cccccccccccccccccccccccccccccccc", MembershipVersion: 3}
	mapping := Mapping{Subject: Subject{Issuer: issuer.Issuer, Value: "provider-subject"}, MembershipRef: ref, IssuerVersion: issuer.Version, Version: 1, Active: true, ExpiresAt: now.Add(time.Hour).Unix()}
	fake.members[ref.MembershipID] = mappingMember{MembershipRef: ref, active: true}
	if err := service.ConfigureIssuer(context.Background(), issuer); err != nil {
		t.Fatal(err)
	}
	return service, fake, issuer, mapping
}

func requireMappingDenied(t *testing.T, service *MappingService, key Subject) {
	t.Helper()
	got, err := service.Lookup(context.Background(), key)
	if !errors.Is(err, ErrMappingNotFound) || got != (MembershipRef{}) {
		t.Fatalf("expected fail-closed lookup, got %+v %v", got, err)
	}
}
