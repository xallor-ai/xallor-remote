package account

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/principal"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

type fakeTransaction struct {
	tenant     Tenant
	person     Principal
	member     Membership
	credential Credential
	revoked    string
}

func (f *fakeTransaction) InsertTenant(_ context.Context, v Tenant) error { f.tenant = v; return nil }
func (f *fakeTransaction) InsertPrincipal(_ context.Context, v Principal) error {
	f.person = v
	return nil
}
func (f *fakeTransaction) InsertMembership(_ context.Context, v Membership) error {
	f.member = v
	return nil
}
func (f *fakeTransaction) LoadMembership(_ context.Context, id string) (Membership, error) {
	if f.member.ID != id {
		return Membership{}, principal.ErrUnauthorized
	}
	return f.member, nil
}
func (f *fakeTransaction) InsertCredential(_ context.Context, v Credential) error {
	f.credential = v
	return nil
}
func (f *fakeTransaction) RevokeCredential(_ context.Context, id string) error {
	f.revoked = id
	return nil
}
func (f *fakeTransaction) SetMembershipActive(_ context.Context, id string, active bool) error {
	if f.member.ID != id {
		return principal.ErrUnauthorized
	}
	f.member.Active = active
	f.member.Version++
	return nil
}

type fakeStore struct {
	state     fakeTransaction
	commitErr error
	calls     int
}

func (*fakeStore) Authority() string { return strings.Repeat("a", 32) }
func (f *fakeStore) TransactAccount(ctx context.Context, operation func(Transaction) error) error {
	f.calls++
	if err := ctx.Err(); err != nil {
		return err
	}
	copy := f.state
	if err := operation(&copy); err != nil {
		return err
	}
	// A commit can have taken effect even when its result is unavailable.
	f.state = copy
	return f.commitErr
}

// Invalid external input must not open a transaction or generate a credential.
func TestService_should_rejectInvalidManagementInput_beforeStore(t *testing.T) {
	f := &fakeStore{}
	s := New(f)
	ctx := context.Background()
	if _, err := s.CreatePrincipal(ctx, CreatePrincipalRequest{Name: "agent", Kind: "user", TenantID: "bad", TTL: time.Hour}); err != ErrInvalidRequest {
		t.Fatal(err)
	}
	if err := s.RevokeCredential(ctx, "bad"); err != ErrInvalidRequest {
		t.Fatal(err)
	}
	if _, err := s.IssueCredential(ctx, IssueCredentialRequest{MembershipID: strings.Repeat("b", 32), TTL: time.Second}); err != ErrInvalidRequest {
		t.Fatal(err)
	}
	if _, err := s.CreateOrganization(ctx, " "); err != ErrInvalidRequest {
		t.Fatal(err)
	}
	if f.calls != 0 {
		t.Fatal("invalid input reached storage")
	}
}

// Application policy creates personal ownership only when no existing space
// was selected; a new member never silently becomes an organization owner.
func TestService_should_chooseSpaceAndRole_and_persistOnlyCredentialHash(t *testing.T) {
	for _, existing := range []string{"", strings.Repeat("b", 32)} {
		t.Run(existing, func(t *testing.T) {
			f := &fakeStore{}
			s := New(f)
			now := time.Unix(1800000000, 0)
			s.now = func() time.Time { return now }
			r, err := s.CreatePrincipal(context.Background(), CreatePrincipalRequest{Name: " agent ", Kind: "service", TenantID: existing, TTL: time.Hour})
			if err != nil {
				t.Fatal(err)
			}
			v := f.state
			if existing == "" {
				if v.tenant.Kind != "personal" || v.tenant.ID != r.TenantID || v.member.Role != "owner" {
					t.Fatal("missing personal ownership", v)
				}
			} else if v.tenant.ID != "" || r.TenantID != existing || v.member.Role != "member" {
				t.Fatal("existing space changed", v)
			}
			if v.person.Name != "agent" || v.person.Kind != "service" || v.person.ID != r.PrincipalID || v.member.ID != r.MembershipID || !v.member.Active || r.MembershipVersion != 1 {
				t.Fatal("incorrect membership")
			}
			if r.Authority != f.Authority() || !principal.ValidToken(r.Token) || v.credential.TokenHash != protocol.SHA256Hex(r.Token) || v.credential.ExpiresAt != now.Add(time.Hour).Unix() || r.ExpiresAt != v.credential.ExpiresAt {
				t.Fatal("incorrect credential")
			}
		})
	}
}

// An ambiguous commit must not return a usable-looking enrollment or retry
// creation, for either a new account or renewal of an existing one.
func TestService_should_returnNoCredential_and_notRetry_whenCommitUnconfirmed(t *testing.T) {
	f := &fakeStore{commitErr: errors.New("commit unconfirmed")}
	s := New(f)
	r, err := s.CreatePrincipal(context.Background(), CreatePrincipalRequest{Name: "agent", Kind: "user", TTL: time.Hour})
	if !errors.Is(err, f.commitErr) || r != (principal.Enrollment{}) || f.calls != 1 {
		t.Fatal("ambiguous creation returned or retried")
	}
	r, err = s.IssueCredential(context.Background(), IssueCredentialRequest{MembershipID: f.state.member.ID, TTL: time.Hour})
	if !errors.Is(err, f.commitErr) || r != (principal.Enrollment{}) || f.calls != 2 {
		t.Fatal("ambiguous renewal returned or retried")
	}
}

// Renewal retains the principal and membership generation. Inactive members
// are rejected before any credential write; reactivation is explicit.
func TestService_should_preserveMembershipOnRenewal_and_rejectInactiveMember(t *testing.T) {
	f := &fakeStore{}
	s := New(f)
	ctx := context.Background()
	a, err := s.CreatePrincipal(ctx, CreatePrincipalRequest{Name: "agent", Kind: "user", TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.IssueCredential(ctx, IssueCredentialRequest{MembershipID: a.MembershipID, TTL: time.Hour})
	if err != nil || b.PrincipalID != a.PrincipalID || b.MembershipID != a.MembershipID || b.MembershipVersion != a.MembershipVersion || b.Token == a.Token {
		t.Fatal("renewal replaced identity", err)
	}
	if err := s.SetMembershipActive(ctx, a.MembershipID, false); err != nil {
		t.Fatal(err)
	}
	before := f.state.credential
	if _, err := s.IssueCredential(ctx, IssueCredentialRequest{MembershipID: a.MembershipID, TTL: time.Hour}); !errors.Is(err, principal.ErrUnauthorized) {
		t.Fatal(err)
	}
	if before != f.state.credential {
		t.Fatal("inactive member received credential")
	}
}
