package storage

import (
	"context"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/account"
	"github.com/xallor-ai/xallor-remote/internal/principal"
)

// A late credential failure must roll back the personal space, principal and
// membership together, even though the application already decided their IDs.
func TestAccount_should_rollbackEnrollment_whenCredentialInsertFails(t *testing.T) {
	s := openIdentityTestStore(t)
	if _, err := s.db.Exec(`CREATE TRIGGER reject_credential BEFORE INSERT ON principal_credentials BEGIN SELECT RAISE(ABORT, 'injected issuance failure'); END`); err != nil {
		t.Fatal(err)
	}
	result, err := account.New(s).CreatePrincipal(context.Background(), account.CreatePrincipalRequest{Name: "alice", Kind: "user", TTL: time.Hour})
	if err == nil || result != (principal.Enrollment{}) {
		t.Fatal("partial enrollment returned")
	}
	for _, table := range []string{"tenants", "principals", "memberships", "principal_credentials"} {
		var count int
		if err := s.db.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("partial state in %s: count=%d err=%v", table, count, err)
		}
	}
}

// Renewal failure leaves the existing credential usable and the membership
// unchanged. Storage rollback must not revoke or replace earlier identity.
func TestAccount_should_preserveExistingCredential_whenRenewalWriteFails(t *testing.T) {
	s := openIdentityTestStore(t)
	ctx := context.Background()
	service := account.New(s)
	first, err := service.CreatePrincipal(ctx, account.CreatePrincipalRequest{Name: "alice", Kind: "user", TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`CREATE TRIGGER reject_renewal BEFORE INSERT ON principal_credentials BEGIN SELECT RAISE(ABORT, 'injected renewal failure'); END`); err != nil {
		t.Fatal(err)
	}
	result, err := service.IssueCredential(ctx, account.IssueCredentialRequest{MembershipID: first.MembershipID, TTL: time.Hour})
	if err == nil || result != (principal.Enrollment{}) {
		t.Fatal("failed renewal returned credential")
	}
	identity, err := s.Authenticate(ctx, first.Token)
	if err != nil || identity != first.Identity {
		t.Fatal("original credential changed", err)
	}
	var count int
	if err := s.db.QueryRow(`SELECT count(*) FROM principal_credentials`).Scan(&count); err != nil || count != 1 {
		t.Fatal("renewal persisted partial state", count, err)
	}
}
