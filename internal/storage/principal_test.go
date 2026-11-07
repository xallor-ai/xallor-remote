package storage

import (
	"context"
	"errors"
	"github.com/xallor-ai/xallor-remote/internal/account"
	"github.com/xallor-ai/xallor-remote/internal/principal"
	"testing"
	"time"
)

// Credentials resolve to authority-owned membership, never a caller-selected
// tenant. Revocation, expiry and member generations are checked on every use.
func TestPrincipal_should_bindAndRevalidate_whenCredentialStateChanges(t *testing.T) {
	s := openIdentityTestStore(t)
	ctx := context.Background()
	a, err := account.New(s).CreatePrincipal(ctx, account.CreatePrincipalRequest{Name: "alice", Kind: "user", TenantID: "", TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	b, err := account.New(s).CreatePrincipal(ctx, account.CreatePrincipalRequest{Name: "agent", Kind: "service", TenantID: a.TenantID, TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if a.PrincipalID == b.PrincipalID || a.TenantID != b.TenantID || a.CredentialID == b.CredentialID {
		t.Fatal("identity scopes collapsed")
	}
	for _, e := range []principal.Enrollment{a, b} {
		got, err := principal.NewService(s).Authenticate(ctx, e.Token)
		if err != nil || got != e.Identity {
			t.Fatalf("authentication: %+v %v", got, err)
		}
	}
	forged := a.Identity
	forged.TenantID = "other"
	if !errors.Is(principal.NewService(s).Verify(ctx, forged), principal.ErrUnauthorized) {
		t.Fatal("accepted self-reported tenant")
	}
	if err := account.New(s).SetMembershipActive(ctx, a.MembershipID, false); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(principal.NewService(s).Verify(ctx, a.Identity), principal.ErrUnauthorized) {
		t.Fatal("disabled membership remained valid")
	}
	if err := account.New(s).SetMembershipActive(ctx, a.MembershipID, true); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(principal.NewService(s).Verify(ctx, a.Identity), principal.ErrUnauthorized) {
		t.Fatal("restoring membership resurrected old connection")
	}
	fresh, err := principal.NewService(s).Authenticate(ctx, a.Token)
	if err != nil || fresh.MembershipVersion <= a.MembershipVersion {
		t.Fatal("fresh membership version missing", err)
	}
	if err := account.New(s).RevokeCredential(ctx, a.CredentialID); err != nil {
		t.Fatal(err)
	}
	if _, err := principal.NewService(s).Authenticate(ctx, a.Token); !errors.Is(err, principal.ErrUnauthorized) {
		t.Fatal("revoked token accepted", err)
	}
	if _, err := s.db.Exec(`UPDATE principal_credentials SET expires_at=? WHERE id=?`, time.Now().Unix()-1, b.CredentialID); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(principal.NewService(s).Verify(ctx, b.Identity), principal.ErrUnauthorized) {
		t.Fatal("expired token accepted")
	}
	var count int
	if err := s.db.QueryRow(`SELECT count(*) FROM principal_credentials WHERE token_hash IN (?,?)`, a.Token, b.Token).Scan(&count); err != nil || count != 0 {
		t.Fatal("plaintext credential persisted", err)
	}
}

// A malformed existing-tenant selection must roll back the principal insert;
// another authority must never authenticate or verify this authority's token.
func TestPrincipal_should_isolateAuthoritiesAndRollback_whenEnrollmentInvalid(t *testing.T) {
	s, other := openIdentityTestStore(t), openIdentityTestStore(t)
	ctx := context.Background()
	if _, err := account.New(s).CreatePrincipal(ctx, account.CreatePrincipalRequest{Name: "invalid", Kind: "user", TenantID: "ffffffffffffffffffffffffffffffff", TTL: time.Hour}); err == nil {
		t.Fatal("orphan membership accepted")
	}
	var count int
	if err := s.db.QueryRow(`SELECT count(*) FROM principals`).Scan(&count); err != nil || count != 0 {
		t.Fatal("partial enrollment persisted", err)
	}
	a, err := account.New(s).CreatePrincipal(ctx, account.CreatePrincipalRequest{Name: "alice", Kind: "user", TenantID: "", TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := principal.NewService(other).Authenticate(ctx, a.Token); !errors.Is(err, principal.ErrUnauthorized) {
		t.Fatal("cross-authority token accepted", err)
	}
	if !errors.Is(principal.NewService(other).Verify(ctx, a.Identity), principal.ErrUnauthorized) {
		t.Fatal("cross-authority context accepted")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := principal.NewService(s).Verify(ctx, a.Identity); err == nil || errors.Is(err, principal.ErrUnauthorized) {
		t.Fatal("storage failure treated as identity decision", err)
	}
}

// Renewal retains a real organization membership, but an inactive membership
// cannot issue a credential or leave a partially inserted token behind.
func TestPrincipal_should_keepMembership_whenCredentialRenewed(t *testing.T) {
	s := openIdentityTestStore(t)
	ctx := context.Background()
	space, err := account.New(s).CreateOrganization(ctx, "team")
	if err != nil {
		t.Fatal(err)
	}
	a, err := account.New(s).CreatePrincipal(ctx, account.CreatePrincipalRequest{Name: "agent", Kind: "service", TenantID: space, TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	b, err := account.New(s).IssueCredential(ctx, account.IssueCredentialRequest{MembershipID: a.MembershipID, TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if a.PrincipalID != b.PrincipalID || b.TenantID != space || a.MembershipID != b.MembershipID || a.Token == b.Token || a.CredentialID == b.CredentialID {
		t.Fatal("renewal changed membership or reused credential")
	}
	if err := account.New(s).RevokeCredential(ctx, a.CredentialID); err != nil {
		t.Fatal(err)
	}
	if _, err := principal.NewService(s).Authenticate(ctx, b.Token); err != nil {
		t.Fatal(err)
	}
	if err := account.New(s).SetMembershipActive(ctx, a.MembershipID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := account.New(s).IssueCredential(ctx, account.IssueCredentialRequest{MembershipID: a.MembershipID, TTL: time.Hour}); !errors.Is(err, principal.ErrUnauthorized) {
		t.Fatal("inactive member received token", err)
	}
	var count int
	if err := s.db.QueryRow(`SELECT count(*) FROM principal_credentials`).Scan(&count); err != nil || count != 2 {
		t.Fatal("failed issuance was not rolled back", count, err)
	}
	if _, err := account.New(s).CreateOrganization(ctx, " "); err == nil {
		t.Fatal("empty space accepted")
	}
}
