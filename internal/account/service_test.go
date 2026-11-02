package account

import (
	"context"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/principal"
)

type fakeStore struct {
	createdName, createdTenant string
	issuedMembership           string
	credentialID, activeID     string
	active                     bool
}

func (f *fakeStore) EnrollPrincipal(_ context.Context, name, _, tenant string, _ time.Duration) (principal.Enrollment, error) {
	f.createdName, f.createdTenant = name, tenant
	return principal.Enrollment{}, nil
}
func (f *fakeStore) RevokeCredential(_ context.Context, id string) error {
	f.credentialID = id
	return nil
}
func (f *fakeStore) CreateOrganization(_ context.Context, name string) (string, error) {
	f.createdName = name
	return "", nil
}
func (f *fakeStore) IssueCredential(_ context.Context, membership string, _ time.Duration) (principal.Enrollment, error) {
	f.issuedMembership = membership
	return principal.Enrollment{}, nil
}
func (f *fakeStore) SetMembershipActive(_ context.Context, id string, active bool) error {
	f.activeID, f.active = id, active
	return nil
}

// Invalid management input is rejected before storage is called, so every
// future transport receives the same use-case validation.
func TestService_should_rejectInvalidManagementInput_beforeStore(t *testing.T) {
	f := &fakeStore{}
	s := New(f)
	if _, err := s.CreatePrincipal(context.Background(), CreatePrincipalRequest{Name: "agent", Kind: "user", TenantID: "bad", TTL: time.Hour}); err != ErrInvalidRequest {
		t.Fatalf("tenant validation error=%v", err)
	}
	if err := s.RevokeCredential(context.Background(), "bad"); err != ErrInvalidRequest {
		t.Fatalf("credential validation error=%v", err)
	}
	if f.createdName != "" || f.credentialID != "" {
		t.Fatalf("store was called for invalid input: %+v", f)
	}
}

// Validated use cases route through one application boundary and preserve
// normalized names before reaching the persistence implementation.
func TestService_should_routeValidatedUseCases_toStore(t *testing.T) {
	f := &fakeStore{}
	s := New(f)
	tenant := "0123456789abcdef0123456789abcdef"
	membership := "abcdef0123456789abcdef0123456789"
	if _, err := s.CreatePrincipal(context.Background(), CreatePrincipalRequest{Name: " agent ", Kind: "user", TenantID: tenant, TTL: time.Hour}); err != nil {
		t.Fatal(err)
	}
	if f.createdName != "agent" || f.createdTenant != tenant {
		t.Fatalf("principal request not normalized: %+v", f)
	}
	if _, err := s.IssueCredential(context.Background(), IssueCredentialRequest{MembershipID: membership, TTL: time.Hour}); err != nil || f.issuedMembership != membership {
		t.Fatalf("credential routing failed: %v %+v", err, f)
	}
	if err := s.SetMembershipActive(context.Background(), membership, false); err != nil || f.activeID != membership || f.active {
		t.Fatalf("membership routing failed: %v %+v", err, f)
	}
}
