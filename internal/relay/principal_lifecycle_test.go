package relay

import (
	"context"
	"errors"
	"github.com/xallor-ai/xallor-remote/internal/access"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/account"
	"github.com/xallor-ai/xallor-remote/internal/principal"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

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
	if _, err := s.Authenticate(ctx, b.Token); err != nil {
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

type blockedPrincipalStore struct {
	*SQLStore
	entered chan struct{}
}

func (s *blockedPrincipalStore) Verify(ctx context.Context, identity principal.Identity) error {
	close(s.entered)
	<-ctx.Done()
	return ctx.Err()
}

// A slow account lookup must not block other connections' routing. Disconnect
// must cancel that lookup, and must never dispatch the waiting invocation.
func TestPrincipal_should_keepRoutingAndCancelLookup_whenVerificationBlocks(t *testing.T) {
	s := &blockedPrincipalStore{SQLStore: openIdentityTestStore(t), entered: make(chan struct{})}
	a, err := account.New(s).CreatePrincipal(context.Background(), account.CreatePrincipalRequest{Name: "alice", Kind: "user", TenantID: "", TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	h := NewHub(s, s, access.NewService(s))
	device, anonymous := &recSender{}, &recSender{}
	if code := h.RegisterDevice("device", "secret", true, device); code != "" {
		t.Fatal(code)
	}
	h.Handle(device, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeDeviceSettings, RequestID: "settings", Inbound: protocol.Bool(true), GrantHash: protocol.SHA256Hex("grant")})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := &contextSender{ctx: ctx}
	if code := h.RegisterClient("device", "grant", a.Token, client); code != "" {
		t.Fatal(code)
	}
	if code := h.RegisterClient("device", "grant", "", anonymous); code != "" {
		t.Fatal(code)
	}
	done := make(chan struct{})
	go func() {
		h.Handle(client, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeInvoke, Op: protocol.OpInfo, ExecID: "blocked"})
		close(done)
	}()
	waitSignal(t, s.entered)
	routed := make(chan struct{})
	go func() {
		h.Handle(anonymous, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeInvoke, Op: protocol.OpInfo, ExecID: "other"})
		close(routed)
	}()
	waitSignal(t, routed)
	if device.last().Type != protocol.TypeInvoke || device.last().ExecID != h.requests[requestKey{client: anonymous, id: "other"}] {
		t.Fatal("unrelated routing stalled")
	}
	cancel()
	waitSignal(t, done)
	if _, ok := h.requests[requestKey{client: client, id: "blocked"}]; ok {
		t.Fatal("disconnected invocation was dispatched")
	}
}
