package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/account"
	"github.com/xallor-ai/xallor-remote/internal/oidc"
)

// A local account enrollment can be mapped from an external subject, while
// stale versions, membership changes and issuer revocation fail closed.
func TestOIDCMapping_shouldResolveAndRevokeAgainstCurrentMembership(t *testing.T) {
	s := openIdentityTestStore(t)
	ctx := context.Background()
	member, err := account.New(s).CreatePrincipal(ctx, account.CreatePrincipalRequest{Name: "oidc-user", Kind: "user", TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	mapper := oidc.NewMappingService(s)
	issuer := oidc.IssuerConfig{Issuer: "https://idp.example", Audience: "xallor", JWKSURL: "https://idp.example/keys", Version: 1, Active: true, ExpiresAt: now.Add(time.Hour).Unix()}
	if err := mapper.ConfigureIssuer(ctx, issuer); err != nil {
		t.Fatal(err)
	}
	key := oidc.Subject{Issuer: issuer.Issuer, Value: "external-1"}
	mapping := oidc.Mapping{Subject: key, MembershipRef: oidc.MembershipRef{TenantID: member.TenantID, PrincipalID: member.PrincipalID, MembershipID: member.MembershipID, MembershipVersion: member.MembershipVersion}, Version: 1, Active: true, ExpiresAt: now.Add(time.Hour).Unix()}
	if err := mapper.Map(ctx, mapping); err != nil {
		t.Fatal(err)
	}
	if got, err := mapper.Lookup(ctx, key); err != nil || got != mapping.MembershipRef {
		t.Fatalf("lookup: %+v %v", got, err)
	}
	if err := mapper.Map(ctx, mapping); !errors.Is(err, oidc.ErrMappingConflict) {
		t.Fatalf("stale mapping accepted: %v", err)
	}
	mapping.Version = 2
	if err := mapper.Map(ctx, mapping); err != nil {
		t.Fatal(err)
	}
	if err := account.New(s).SetMembershipActive(ctx, member.MembershipID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := mapper.Lookup(ctx, key); !errors.Is(err, oidc.ErrMappingNotFound) {
		t.Fatalf("inactive membership resolved: %v", err)
	}
	if err := account.New(s).SetMembershipActive(ctx, member.MembershipID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := mapper.Lookup(ctx, key); !errors.Is(err, oidc.ErrMappingNotFound) {
		t.Fatalf("old membership version resolved after rotation: %v", err)
	}
	if err := mapper.RevokeIssuer(ctx, issuer.Issuer, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := mapper.Lookup(ctx, key); !errors.Is(err, oidc.ErrMappingNotFound) {
		t.Fatalf("revoked issuer resolved: %v", err)
	}
}
