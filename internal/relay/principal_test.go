package relay

import (
	"context"
	"github.com/xallor-ai/xallor-remote/internal/access"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/account"
	"github.com/xallor-ai/xallor-remote/internal/admission"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// An already authenticated connection must lose new-invocation permission after
// token revocation; shared device authorization cannot downgrade that connection.
func TestPrincipal_should_rejectNewInvoke_whenCredentialRevoked(t *testing.T) {
	s := openIdentityTestStore(t)
	a, err := account.New(s).CreatePrincipal(context.Background(), account.CreatePrincipalRequest{Name: "alice", Kind: "user", TenantID: "", TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	h := NewHub(s, s, access.NewService(s), nil, admission.New(admission.Limits{PerDevice: protocol.MaxDeviceRequests}))
	dev, client := &recSender{}, &recSender{}
	if code := h.RegisterDevice("device", "secret", true, dev); code != "" {
		t.Fatal(code)
	}
	h.Handle(dev, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeDeviceSettings, RequestID: "settings", Inbound: protocol.Bool(true), GrantHash: protocol.SHA256Hex("grant")})
	if code := h.RegisterClient("device", "grant", a.Token, client); code != "" {
		t.Fatal(code)
	}
	h.Handle(client, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeInvoke, Op: protocol.OpInfo, ExecID: "before", AccountToken: "not-forwarded"})
	if h.inflight[dev.last().ExecID].binding.identity != a.Identity || dev.last().AccountToken != "" {
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
