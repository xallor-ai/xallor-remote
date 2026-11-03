package relay

import (
	"context"
	"errors"
	"github.com/xallor-ai/xallor-remote/internal/access"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/account"
	"github.com/xallor-ai/xallor-remote/internal/admission"
	"github.com/xallor-ai/xallor-remote/internal/principal"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
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
		got, err := s.Authenticate(ctx, e.Token)
		if err != nil || got != e.Identity {
			t.Fatalf("authentication: %+v %v", got, err)
		}
	}
	forged := a.Identity
	forged.TenantID = "other"
	if !errors.Is(s.Verify(ctx, forged), principal.ErrUnauthorized) {
		t.Fatal("accepted self-reported tenant")
	}
	if err := account.New(s).SetMembershipActive(ctx, a.MembershipID, false); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(s.Verify(ctx, a.Identity), principal.ErrUnauthorized) {
		t.Fatal("disabled membership remained valid")
	}
	if err := account.New(s).SetMembershipActive(ctx, a.MembershipID, true); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(s.Verify(ctx, a.Identity), principal.ErrUnauthorized) {
		t.Fatal("restoring membership resurrected old connection")
	}
	fresh, err := s.Authenticate(ctx, a.Token)
	if err != nil || fresh.MembershipVersion <= a.MembershipVersion {
		t.Fatal("fresh membership version missing", err)
	}
	if err := account.New(s).RevokeCredential(ctx, a.CredentialID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(ctx, a.Token); !errors.Is(err, principal.ErrUnauthorized) {
		t.Fatal("revoked token accepted", err)
	}
	if _, err := s.db.Exec(`UPDATE principal_credentials SET expires_at=? WHERE id=?`, time.Now().Unix()-1, b.CredentialID); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(s.Verify(ctx, b.Identity), principal.ErrUnauthorized) {
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
	if _, err := other.Authenticate(ctx, a.Token); !errors.Is(err, principal.ErrUnauthorized) {
		t.Fatal("cross-authority token accepted", err)
	}
	if !errors.Is(other.Verify(ctx, a.Identity), principal.ErrUnauthorized) {
		t.Fatal("cross-authority context accepted")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Verify(ctx, a.Identity); err == nil || errors.Is(err, principal.ErrUnauthorized) {
		t.Fatal("storage failure treated as identity decision", err)
	}
}

// An already authenticated connection must lose new-invocation permission after
// token revocation; shared device authorization cannot downgrade that connection.
func TestPrincipal_should_rejectNewInvoke_whenCredentialRevoked(t *testing.T) {
	s := openIdentityTestStore(t)
	a, err := account.New(s).CreatePrincipal(context.Background(), account.CreatePrincipalRequest{Name: "alice", Kind: "user", TenantID: "", TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	h := NewHub(s, s, access.NewService(s), admission.New(admission.Limits{PerDevice: protocol.MaxDeviceRequests}))
	dev, client := &recSender{}, &recSender{}
	if code := h.RegisterDevice("device", "secret", true, dev); code != "" {
		t.Fatal(code)
	}
	h.Handle(dev, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeDeviceSettings, RequestID: "settings", Inbound: protocol.Bool(true), GrantHash: protocol.SHA256Hex("grant")})
	if code := h.RegisterClient("device", "grant", a.Token, client); code != "" {
		t.Fatal(code)
	}
	h.Handle(client, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeInvoke, Op: protocol.OpInfo, ExecID: "before", AccountToken: "not-forwarded"})
	if h.inflight[dev.last().ExecID].identity != a.Identity || dev.last().AccountToken != "" {
		t.Fatal("trusted invocation context missing or token forwarded")
	}
	if err := account.New(s).RevokeCredential(context.Background(), a.CredentialID); err != nil {
		t.Fatal(err)
	}
	h.Handle(client, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeInvoke, Op: protocol.OpInfo, ExecID: "after"})
	if client.last().Code != protocol.Unauthorized || h.requests[requestKey{client: client, id: "after"}] != "" {
		t.Fatal("revoked connection retained authorization")
	}
	if code := h.RegisterClient("device", "grant", a.Token, &recSender{}); code != protocol.Unauthorized {
		t.Fatal("revoked hello downgraded to bearer", code)
	}
	h.Handle(client, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeInvoke, Op: protocol.OpCancel, ExecID: "before"})
	if dev.last().Op != protocol.OpCancel {
		t.Fatal("revocation prevented cancellation of original task")
	}
}
