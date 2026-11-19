package delegation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/accesscode"
	"github.com/xallor-ai/xallor-remote/internal/principal"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

type sessionAuthorityStore struct {
	accesscode.Store
	accesscode.Reader
	session accesscode.SessionAuthorization
}

func (s *sessionAuthorityStore) ReadAccessCode(_ context.Context, read func(accesscode.Reader) error) error {
	return read(s)
}

func (s *sessionAuthorityStore) Session(context.Context, string) (accesscode.SessionAuthorization, error) {
	return s.session, nil
}

func sessionAuthorityForTest(t *testing.T) (*Authority, *authorityFixture, *sessionAuthorityStore, *time.Time) {
	t.Helper()
	a, f, _ := authorityForTest(t)
	now := time.Unix(1000, 0).UTC()
	a.now = func() time.Time { return now }
	f.identity = principal.Identity{PrincipalID: "subject"}
	expires := now.Add(2 * time.Second)
	s := &sessionAuthorityStore{session: accesscode.SessionAuthorization{
		ID: "session", CodeID: "code", Generation: 1, State: accesscode.SessionActive,
		IssuedAt: now, ExpiresAt: &expires, Scope: []string{protocol.OpHandoffSubmit},
		Target: accesscode.TargetBinding{DeviceUID: f.device.UID, Epoch: "recipient-epoch"},
	}}
	a.SetSessionStore(s)
	return a, f, s, &now
}

// A proof cannot lease execution beyond its original consent deadline, even
// when a later storage read has a longer deadline; shorter deadlines also bind.
func TestSessionAuthority_shouldBoundLease_byOriginalAndCurrentConsentExpiry(t *testing.T) {
	for _, change := range []string{"unchanged", "extended", "shortened", "removed"} {
		t.Run(change, func(t *testing.T) {
			a, f, s, now := sessionAuthorityForTest(t)
			proof, err := a.IssueSession("123456789", f.device.Binding(), f.identity, f.authorization, s.session)
			if err != nil {
				t.Fatal(err)
			}
			want := 2 * time.Second
			switch change {
			case "extended":
				expires := now.Add(time.Hour)
				s.session.ExpiresAt = &expires
			case "shortened":
				expires := now.Add(time.Second)
				s.session.ExpiresAt = &expires
				want = time.Second
			case "removed":
				s.session.ExpiresAt = nil
			}
			ttl, err := a.Renew(context.Background(), "123456789", f.device.UID, proof)
			if err != nil || ttl != want {
				t.Fatalf("lease=%v err=%v, want %v", ttl, err, want)
			}
		})
	}
}

// A permission lookup may take longer than the remaining consent lifetime.
// The final lease decision must use the clock after that lookup completes.
func TestSessionAuthority_shouldDenyRenewal_whenPermissionReadCrossesConsentExpiry(t *testing.T) {
	for _, shortened := range []bool{false, true} {
		t.Run(map[bool]string{false: "original", true: "shortened_after_issue"}[shortened], func(t *testing.T) {
			a, f, s, now := sessionAuthorityForTest(t)
			deadline := *s.session.ExpiresAt
			if shortened {
				s.session.ExpiresAt = nil
			}
			proof, err := a.IssueSession("123456789", f.device.Binding(), f.identity, f.authorization, s.session)
			if err != nil {
				t.Fatal(err)
			}
			s.session.ExpiresAt = &deadline
			f.after = func() { *now = now.Add(3 * time.Second) }
			ttl, err := a.Renew(context.Background(), "123456789", f.device.UID, proof)
			if !errors.Is(err, ErrDenied) || ttl != 0 {
				t.Fatalf("expired consent renewed: %v %v", ttl, err)
			}
		})
	}
}

// An unlimited session still receives only the normal bounded execution lease.
func TestSessionAuthority_shouldKeepBoundedLease_whenConsentHasNoExpiry(t *testing.T) {
	a, f, s, _ := sessionAuthorityForTest(t)
	s.session.ExpiresAt = nil
	proof, err := a.IssueSession("123456789", f.device.Binding(), f.identity, f.authorization, s.session)
	if err != nil {
		t.Fatal(err)
	}
	ttl, err := a.Renew(context.Background(), "123456789", f.device.UID, proof)
	if err != nil || ttl != protocol.ExecutionLeaseDuration {
		t.Fatal(ttl, err)
	}
}

// Session proof issuance must retain the same configured-authority and valid
// binding requirements as ordinary proof issuance, without panicking.
func TestSessionAuthority_shouldRefuseIssue_whenAuthorityOrBindingIsInvalid(t *testing.T) {
	for _, kind := range []string{"devices", "key", "binding"} {
		t.Run(kind, func(t *testing.T) {
			a, f, s, _ := sessionAuthorityForTest(t)
			binding := f.device.Binding()
			switch kind {
			case "devices":
				a.devices = nil
			case "key":
				a.key = [32]byte{}
			case "binding":
				binding.GrantID = ""
			}
			defer func() {
				if p := recover(); p != nil {
					t.Errorf("invalid issuer panicked: %v", p)
				}
			}()
			proof, err := a.IssueSession("123456789", binding, f.identity, f.authorization, s.session)
			if proof != "" || !errors.Is(err, ErrDenied) {
				t.Fatal("invalid authority issued proof", err)
			}
		})
	}
}
