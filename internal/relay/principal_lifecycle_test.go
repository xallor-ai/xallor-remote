package relay

import (
	"context"
	"github.com/xallor-ai/xallor-remote/internal/access"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/account"
	"github.com/xallor-ai/xallor-remote/internal/admission"
	"github.com/xallor-ai/xallor-remote/internal/principal"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

type blockedPrincipalStore struct {
	*relaySQLFixture
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
	s := &blockedPrincipalStore{relaySQLFixture: openIdentityTestStore(t), entered: make(chan struct{})}
	a, err := account.New(s).CreatePrincipal(context.Background(), account.CreatePrincipalRequest{Name: "alice", Kind: "user", TenantID: "", TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	h := NewHub(s, s, access.NewService(s), nil, admission.New(admission.Limits{PerDevice: protocol.MaxDeviceRequests}))
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
