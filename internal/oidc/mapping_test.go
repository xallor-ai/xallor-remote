package oidc

import (
	"context"
	"errors"
	"testing"
)

// A binding resolves only for its exact issuer+subject key, and rejected
// duplicate/stale writes must leave the existing local membership unchanged.
func TestMappingService_shouldResolveExactSubjectAndRejectStaleWrites(t *testing.T) {
	service, fake, issuer, mapping := mappingFixture(t)
	ctx := context.Background()
	if err := service.Map(ctx, mapping); err != nil {
		t.Fatal(err)
	}
	got, err := service.Lookup(ctx, mapping.Subject)
	if err != nil || got != mapping.MembershipRef {
		t.Fatalf("lookup: %+v %v", got, err)
	}
	for _, version := range []int64{mapping.Version, mapping.Version - 1} {
		duplicate := mapping
		duplicate.Version = version
		if err := service.Map(ctx, duplicate); err == nil || fake.mappings[mapping.Subject] != mapping {
			t.Fatalf("stale write changed binding: %+v %v", duplicate, err)
		}
	}
	requireMappingDenied(t, service, Subject{Issuer: issuer.Issuer, Value: "other-subject"})
	otherIssuer := issuer
	otherIssuer.Issuer, otherIssuer.JWKSURL = "https://other.example", "https://other.example/keys"
	if err := service.ConfigureIssuer(ctx, otherIssuer); err != nil {
		t.Fatal(err)
	}
	requireMappingDenied(t, service, Subject{Issuer: otherIssuer.Issuer, Value: mapping.Value})
	otherMapping := mapping
	otherMapping.Issuer = otherIssuer.Issuer
	if err := service.Map(ctx, otherMapping); err != nil || len(fake.mappings) != 2 {
		t.Fatalf("distinct issuer key collapsed: %v", err)
	}
}

// Inactive, invalid or expired trust metadata must reject reads and new writes
// at the exact expiration boundary, even if a previously valid binding exists.
func TestMappingService_shouldDenyIssuerAndMappingStateOnEveryUse(t *testing.T) {
	for name, mutate := range map[string]func(*IssuerConfig){
		"inactive":        func(c *IssuerConfig) { c.Active = false },
		"expired":         func(c *IssuerConfig) { c.ExpiresAt = 19_999 },
		"expiry boundary": func(c *IssuerConfig) { c.ExpiresAt = 20_000 },
		"zero version":    func(c *IssuerConfig) { c.Version = 0 },
		"bad audience":    func(c *IssuerConfig) { c.Audience = "" },
		"bad issuer":      func(c *IssuerConfig) { c.Issuer = "http://issuer.example" },
		"bad JWKS":        func(c *IssuerConfig) { c.JWKSURL = "http://issuer.example/keys" },
	} {
		t.Run("issuer/"+name, func(t *testing.T) {
			service, fake, issuer, mapping := mappingFixture(t)
			ctx := context.Background()
			if err := service.Map(ctx, mapping); err != nil {
				t.Fatal(err)
			}
			invalid := issuer
			invalid.Version++
			mutate(&invalid)
			if err := service.ConfigureIssuer(ctx, invalid); !errors.Is(err, ErrMappingInvalid) || fake.issuers[issuer.Issuer] != issuer {
				t.Fatalf("invalid configuration changed store: %v", err)
			}
			fake.issuers[issuer.Issuer] = invalid
			requireMappingDenied(t, service, mapping.Subject)
			mapping.Value = "new-subject"
			if err := service.Map(ctx, mapping); !errors.Is(err, ErrMappingNotFound) {
				t.Fatalf("invalid issuer admitted mapping: %v", err)
			}
		})
	}
	for name, mutate := range map[string]func(*Mapping){
		"inactive":        func(m *Mapping) { m.Active = false },
		"expired":         func(m *Mapping) { m.ExpiresAt = 19_999 },
		"expiry boundary": func(m *Mapping) { m.ExpiresAt = 20_000 },
		"zero version":    func(m *Mapping) { m.Version = 0 },
	} {
		t.Run("mapping/"+name, func(t *testing.T) {
			service, fake, _, mapping := mappingFixture(t)
			invalid := mapping
			mutate(&invalid)
			if err := service.Map(context.Background(), invalid); !errors.Is(err, ErrMappingInvalid) || len(fake.mappings) != 0 {
				t.Fatalf("invalid mapping persisted: %v", err)
			}
			fake.mappings[mapping.Subject] = invalid
			requireMappingDenied(t, service, mapping.Subject)
		})
	}
}

// All local scope fields and membership generation are checked by the adapter
// snapshot on writes and reads; member reactivation cannot revive an old map.
func TestMappingService_shouldRejectScopeOrMembershipVersionMismatch(t *testing.T) {
	for name, mutate := range map[string]func(*mappingMember){
		"tenant":     func(m *mappingMember) { m.TenantID = "dddddddddddddddddddddddddddddddd" },
		"principal":  func(m *mappingMember) { m.PrincipalID = "dddddddddddddddddddddddddddddddd" },
		"membership": func(m *mappingMember) { m.MembershipID = "dddddddddddddddddddddddddddddddd" },
		"version":    func(m *mappingMember) { m.MembershipVersion++ },
		"inactive":   func(m *mappingMember) { m.active = false },
	} {
		t.Run(name, func(t *testing.T) {
			service, fake, _, mapping := mappingFixture(t)
			ctx := context.Background()
			if err := service.Map(ctx, mapping); err != nil {
				t.Fatal(err)
			}
			member := fake.members[mapping.MembershipID]
			mutate(&member)
			fake.members[mapping.MembershipID] = member
			requireMappingDenied(t, service, mapping.Subject)
			mapping.Version++
			if err := service.Map(ctx, mapping); !errors.Is(err, ErrMappingNotFound) {
				t.Fatalf("scope mismatch persisted: %v", err)
			}
		})
	}
}

// Revocation keeps a version tombstone. Older config or map writes cannot
// restore access; an explicit newer binding to an active member can.
func TestMappingService_shouldRequireNewerVersionsAfterRevokeAndRotation(t *testing.T) {
	service, fake, issuer, mapping := mappingFixture(t)
	ctx := context.Background()
	if err := service.Map(ctx, mapping); err != nil {
		t.Fatal(err)
	}
	if err := service.Revoke(ctx, mapping.Subject, 1); !errors.Is(err, ErrMappingConflict) {
		t.Fatalf("stale revoke: %v", err)
	}
	if err := service.Revoke(ctx, mapping.Subject, 2); err != nil {
		t.Fatal(err)
	}
	requireMappingDenied(t, service, mapping.Subject)
	mapping.Version = 2
	if err := service.Map(ctx, mapping); !errors.Is(err, ErrMappingConflict) {
		t.Fatalf("revoked binding restored: %v", err)
	}
	mapping.Version = 3
	if err := service.Map(ctx, mapping); err != nil {
		t.Fatal(err)
	}
	if got, err := service.Lookup(ctx, mapping.Subject); err != nil || got != mapping.MembershipRef {
		t.Fatalf("fresh binding: %+v %v", got, err)
	}
	if err := service.RevokeIssuer(ctx, issuer.Issuer, 1); !errors.Is(err, ErrMappingConflict) {
		t.Fatalf("stale issuer revoke: %v", err)
	}
	if err := service.RevokeIssuer(ctx, issuer.Issuer, 2); err != nil {
		t.Fatal(err)
	}
	requireMappingDenied(t, service, mapping.Subject)
	issuer.Version = 2
	if err := service.ConfigureIssuer(ctx, issuer); !errors.Is(err, ErrMappingConflict) {
		t.Fatalf("revoked issuer restored: %v", err)
	}
	issuer.Version = 3
	issuer.Audience, issuer.JWKSURL = "rotated-audience", "https://issuer.example/rotated-keys"
	if err := service.ConfigureIssuer(ctx, issuer); err != nil || fake.issuers[issuer.Issuer] != issuer {
		t.Fatalf("issuer rotation: %v", err)
	}
	requireMappingDenied(t, service, mapping.Subject)
	mapping.Version++
	if err := service.Map(ctx, mapping); err != nil {
		t.Fatalf("fresh binding to rotated issuer: %v", err)
	}
	if got, err := service.Lookup(ctx, mapping.Subject); err != nil || got != mapping.MembershipRef {
		t.Fatalf("fresh issuer binding lookup: %+v %v", got, err)
	}
	issuer.Version--
	if err := service.ConfigureIssuer(ctx, issuer); !errors.Is(err, ErrMappingConflict) {
		t.Fatalf("old config overwrote rotation: %v", err)
	}
}

// Storage errors and missing rows must never return a usable local identity or
// mutate existing configuration, even after a previous successful lookup.
func TestMappingService_shouldFailClosedWhenStoreIsUnavailableOrMissing(t *testing.T) {
	for _, operation := range []string{"put issuer", "put mapping", "revoke issuer", "revoke mapping", "load issuer", "lookup mapping"} {
		t.Run(operation, func(t *testing.T) {
			service, fake, issuer, mapping := mappingFixture(t)
			ctx := context.Background()
			if err := service.Map(ctx, mapping); err != nil {
				t.Fatal(err)
			}
			fake.fail = operation
			var err error
			switch operation {
			case "put issuer":
				issuer.Version++
				err = service.ConfigureIssuer(ctx, issuer)
			case "put mapping":
				mapping.Version++
				err = service.Map(ctx, mapping)
			case "revoke issuer":
				err = service.RevokeIssuer(ctx, issuer.Issuer, 2)
			case "revoke mapping":
				err = service.Revoke(ctx, mapping.Subject, 2)
			default:
				requireMappingDenied(t, service, mapping.Subject)
				return
			}
			if !errors.Is(err, fake.err) {
				t.Fatalf("write failure hidden: %v", err)
			}
		})
	}
	service, fake, issuer, mapping := mappingFixture(t)
	requireMappingDenied(t, service, mapping.Subject)
	if err := service.Revoke(context.Background(), mapping.Subject, 1); !errors.Is(err, ErrMappingNotFound) {
		t.Fatalf("missing mapping revoke: %v", err)
	}
	delete(fake.issuers, issuer.Issuer)
	requireMappingDenied(t, service, mapping.Subject)
	if err := service.Map(context.Background(), mapping); !errors.Is(err, ErrMappingNotFound) {
		t.Fatalf("missing issuer map: %v", err)
	}
}

// Malformed or incomplete local identities must be rejected before the store
// can persist them; missing service dependencies must not panic or authorize.
func TestMappingService_shouldRejectInvalidInputsAndMissingStore(t *testing.T) {
	for name, mutate := range map[string]func(*Mapping){
		"empty issuer":            func(m *Mapping) { m.Issuer = "" },
		"empty subject":           func(m *Mapping) { m.Value = " " },
		"bad tenant":              func(m *Mapping) { m.TenantID = "tenant" },
		"bad principal":           func(m *Mapping) { m.PrincipalID = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA" },
		"bad membership":          func(m *Mapping) { m.MembershipID = "0000000000000000000000000000000!" },
		"zero membership version": func(m *Mapping) { m.MembershipVersion = 0 },
		"negative version":        func(m *Mapping) { m.Version = -1 },
		"missing expiry":          func(m *Mapping) { m.ExpiresAt = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			service, fake, _, mapping := mappingFixture(t)
			mutate(&mapping)
			if err := service.Map(context.Background(), mapping); !errors.Is(err, ErrMappingInvalid) || len(fake.mappings) != 0 {
				t.Fatalf("invalid input persisted: %+v %v", mapping, err)
			}
		})
	}
	_, _, issuer, mapping := mappingFixture(t)
	ctx := context.Background()
	for _, service := range []*MappingService{nil, NewMappingService(nil)} {
		if err := service.ConfigureIssuer(ctx, issuer); !errors.Is(err, ErrMappingInvalid) {
			t.Fatalf("configure without store: %v", err)
		}
		if err := service.Map(ctx, mapping); !errors.Is(err, ErrMappingInvalid) {
			t.Fatalf("map without store: %v", err)
		}
		if err := service.RevokeIssuer(ctx, issuer.Issuer, 2); !errors.Is(err, ErrMappingInvalid) {
			t.Fatalf("issuer revoke without store: %v", err)
		}
		if err := service.Revoke(ctx, mapping.Subject, 2); !errors.Is(err, ErrMappingInvalid) {
			t.Fatalf("revoke without store: %v", err)
		}
		requireMappingDenied(t, service, mapping.Subject)
	}
}
