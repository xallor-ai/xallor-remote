package principal

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

type credentialReader struct {
	byID   func(context.Context, string) (Credential, error)
	byHash func(context.Context, string) (Credential, error)
}

func (r credentialReader) CredentialByID(ctx context.Context, id string) (Credential, error) {
	return r.byID(ctx, id)
}
func (r credentialReader) CredentialByTokenHash(ctx context.Context, hash string) (Credential, error) {
	return r.byHash(ctx, hash)
}

func validCredential() Credential {
	return Credential{Identity: Identity{Authority: "authority", PrincipalID: "person", TenantID: "tenant", MembershipID: "member", MembershipVersion: 1, CredentialID: "credential", ExpiresAt: 2000}, MembershipActive: true}
}

// Storage supplies raw facts. Revocation, inactive membership and expiry must
// deny both hello and revalidation, and no raw token may enter the reader.
func TestService_should_applyCredentialPolicy_whenStorageReturnsUnfilteredState(t *testing.T) {
	token := "xr_account_" + strings.Repeat("a", 64)
	for _, condition := range []string{"valid", "revoked", "inactive", "expired", "at-deadline"} {
		t.Run(condition, func(t *testing.T) {
			row := validCredential()
			switch condition {
			case "revoked":
				row.Revoked = true
			case "inactive":
				row.MembershipActive = false
			case "expired":
				row.ExpiresAt = 999
			case "at-deadline":
				row.ExpiresAt = 1000
			}
			s := NewService(credentialReader{
				byID: func(_ context.Context, id string) (Credential, error) {
					if id != row.CredentialID {
						t.Fatal("wrong credential lookup")
					}
					return row, nil
				},
				byHash: func(_ context.Context, hash string) (Credential, error) {
					if hash != protocol.SHA256Hex(token) {
						t.Fatal("raw token or incorrect digest reached storage")
					}
					return row, nil
				},
			})
			s.now = func() time.Time { return time.Unix(1000, 0) }
			identity, authErr := s.Authenticate(context.Background(), token)
			verifyErr := s.Verify(context.Background(), row.Identity)
			if condition == "valid" {
				if authErr != nil || verifyErr != nil || identity != row.Identity {
					t.Fatal("valid identity denied", authErr, verifyErr)
				}
			} else if !errors.Is(authErr, ErrUnauthorized) || !errors.Is(verifyErr, ErrUnauthorized) || identity != (Identity{}) {
				t.Fatal("invalid credential leaked identity or was authorized", authErr, verifyErr)
			}
		})
	}
}

// A resumed membership or caller-forged context must not reuse a bound
// connection. Every identity component participates in the comparison.
func TestCredential_should_rejectStaleBinding_whenAnyIdentityFieldChanges(t *testing.T) {
	row := validCredential()
	changes := []func(*Identity){
		func(p *Identity) { p.Authority = "another" },
		func(p *Identity) { p.PrincipalID = "another" },
		func(p *Identity) { p.TenantID = "another" },
		func(p *Identity) { p.MembershipID = "another" },
		func(p *Identity) { p.MembershipVersion++ },
		func(p *Identity) { p.CredentialID = "another" },
		func(p *Identity) { p.ExpiresAt++ },
	}
	for i, change := range changes {
		expected := row.Identity
		change(&expected)
		if !errors.Is(row.Verify(expected, 1000), ErrUnauthorized) {
			t.Fatalf("changed field %d authorized", i)
		}
	}
}

// The clock advances inside the reader, modeling pool/lock wait without sleeps.
// Operational failures and cancellation remain distinguishable from denial.
func TestService_should_checkTimeAfterRead_and_preserveStorageFailures(t *testing.T) {
	token := "xr_account_" + strings.Repeat("a", 64)
	for _, operation := range []string{"authenticate", "verify"} {
		for _, condition := range []string{"expiry", "storage", "cancelled"} {
			t.Run(operation+"/"+condition, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				now, row := int64(1000), validCredential()
				storageErr := errors.New("storage unavailable")
				read := func(context.Context, string) (Credential, error) {
					now = row.ExpiresAt
					if condition == "storage" {
						return row, storageErr
					}
					if condition == "cancelled" {
						cancel()
					}
					return row, nil
				}
				s := NewService(credentialReader{byID: read, byHash: read})
				s.now = func() time.Time { return time.Unix(now, 0) }
				var err error
				if operation == "authenticate" {
					var identity Identity
					identity, err = s.Authenticate(ctx, token)
					if identity != (Identity{}) {
						t.Fatal("failed authentication exposed identity")
					}
				} else {
					err = s.Verify(ctx, row.Identity)
				}
				want := ErrUnauthorized
				if condition == "storage" {
					want = storageErr
				}
				if condition == "cancelled" {
					want = context.Canceled
				}
				if !errors.Is(err, want) {
					t.Fatalf("got %v, want %v", err, want)
				}
			})
		}
	}
}

// Syntax rejection happens before storage: malformed caller input cannot consume
// a credential lookup or be downgraded to another authentication path.
func TestService_should_rejectMalformedToken_withoutStorageRead(t *testing.T) {
	s := NewService(credentialReader{})
	for _, token := range []string{"", "xr_account_short", "xr_account_" + strings.Repeat("A", 64)} {
		if _, err := s.Authenticate(context.Background(), token); !errors.Is(err, ErrUnauthorized) {
			t.Fatal(err)
		}
	}
}
