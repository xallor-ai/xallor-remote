package oidc

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// Real local RSA verification feeds only issuer+subject to the mapper. Forged
// provider tenant/principal/member/role claims cannot select local scope, and
// revocation immediately denies the same still-valid provider token.
func TestMappingService_shouldUseLocalScopeAfterSignedProviderVerification(t *testing.T) {
	service, fake, issuer, mapping := mappingFixture(t)
	ctx := context.Background()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := NewKeySet([]JWK{jwk("local-test", key)})
	if err != nil {
		t.Fatal(err)
	}
	provider := map[string]any{"iss": issuer.Issuer, "sub": mapping.Value, "aud": issuer.Audience, "exp": mapping.ExpiresAt,
		"tenant_id": "dddddddddddddddddddddddddddddddd", "principal_id": "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
		"membership_id": "ffffffffffffffffffffffffffffffff", "membership_version": 99, "role": "owner"}
	signed := token(t, key, "local-test", provider, "RS256")
	verified, err := keys.Verify(signed, issuer.Verifier(), service.now())
	if err != nil {
		t.Fatal(err)
	}
	if verified.SubjectKey() != mapping.Subject {
		t.Fatalf("subject changed: %+v", verified)
	}
	raw, err := json.Marshal(verified)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"tenant", "principal", "membership", "role"} {
		if strings.Contains(strings.ToLower(string(raw)), forbidden) {
			t.Fatalf("provider authority retained: %s", raw)
		}
	}
	requireMappingDenied(t, service, verified.SubjectKey())
	if err := service.Map(ctx, mapping); err != nil {
		t.Fatal(err)
	}
	got, err := service.Lookup(ctx, verified.SubjectKey())
	if err != nil || got != mapping.MembershipRef || got.TenantID == provider["tenant_id"] || got.PrincipalID == provider["principal_id"] {
		t.Fatalf("provider scope replaced local membership: %+v %v", got, err)
	}
	if len(fake.mappings) != 1 || fake.mappings[mapping.Subject] != mapping {
		t.Fatal("provider claims changed local mapping")
	}
	if err := service.Revoke(ctx, mapping.Subject, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := keys.Verify(signed, issuer.Verifier(), service.now()); err != nil {
		t.Fatal("local revoke changed provider validity", err)
	}
	requireMappingDenied(t, service, verified.SubjectKey())
	// Issuer configuration rotation changes verification policy immediately;
	// the mapping itself never bypasses the new issuer/audience/key policy.
	issuer.Version, issuer.Audience = 2, "rotated-audience"
	if err := service.ConfigureIssuer(ctx, issuer); err != nil {
		t.Fatal(err)
	}
	if _, err := keys.Verify(signed, issuer.Verifier(), service.now()); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("old audience survived rotation: %v", err)
	}
}

// A corrupt/misbehaving adapter must not substitute another issuer or subject
// row, even when that row happens to reference a valid local membership.
func TestMappingService_shouldDenyWhenStoreReturnsDifferentIdentityKey(t *testing.T) {
	for _, component := range []string{"issuer config", "mapping issuer", "mapping subject", "mapping local ID", "mapping issuer version"} {
		t.Run(component, func(t *testing.T) {
			service, fake, issuer, mapping := mappingFixture(t)
			if err := service.Map(context.Background(), mapping); err != nil {
				t.Fatal(err)
			}
			switch component {
			case "issuer config":
				other := issuer
				other.Issuer = "https://other.example"
				fake.issuers[issuer.Issuer] = other
				if err := service.Map(context.Background(), mapping); !errors.Is(err, ErrMappingNotFound) {
					t.Fatalf("wrong issuer admitted mapping: %v", err)
				}
			case "mapping issuer":
				other := mapping
				other.Issuer = "https://other.example"
				fake.mappings[mapping.Subject] = other
			case "mapping subject":
				other := mapping
				other.Value = "other-subject"
				fake.mappings[mapping.Subject] = other
			case "mapping issuer version":
				other := mapping
				other.IssuerVersion++
				fake.mappings[mapping.Subject] = other
			case "mapping local ID":
				invalid := mapping
				invalid.TenantID = "invalid-local-id"
				fake.mappings[mapping.Subject] = invalid
				member := fake.members[mapping.MembershipID]
				member.TenantID = invalid.TenantID
				fake.members[mapping.MembershipID] = member
			}
			requireMappingDenied(t, service, mapping.Subject)
		})
	}
}
