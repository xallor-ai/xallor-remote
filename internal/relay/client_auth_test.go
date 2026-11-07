package relay

import (
	"context"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/account"
	"github.com/xallor-ai/xallor-remote/internal/admission"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Relay confirmation describes the authentication that actually succeeded.
// A valid device grant cannot turn invalid or revoked account auth into shared auth.
func TestClient_should_confirmValidatedMode_andRejectInvalidAccount(t *testing.T) {
	s := openIdentityTestStore(t)
	a, err := account.New(s).CreatePrincipal(context.Background(), account.CreatePrincipalRequest{Name: "alice", Kind: "user", TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	h := NewHub(s, s, access.NewService(s), nil, admission.New(admission.Limits{PerDevice: protocol.MaxDeviceRequests}))
	device := &recSender{}
	if code := h.RegisterDevice("123456789", "secret", true, device); code != "" {
		t.Fatal(code)
	}
	h.Handle(device, protocol.RoleDevice, "123456789", protocol.Message{Type: protocol.TypeDeviceSettings, RequestID: "settings", Inbound: protocol.Bool(true), GrantHash: protocol.SHA256Hex("grant")})
	for _, token := range []string{"", a.Token} {
		client := &recSender{}
		if code := h.RegisterClient("123456789", "grant", token, client); code != "" {
			t.Fatal(code)
		}
		if !client.last().ConfirmsClientAuth(token != "") {
			t.Fatalf("wrong confirmation: %+v", client.last())
		}
	}
	if err := account.New(s).RevokeCredential(context.Background(), a.CredentialID); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"invalid", a.Token} {
		client := &recSender{}
		if code := h.RegisterClient("123456789", "grant", token, client); code != protocol.Unauthorized {
			t.Fatal("account auth was not rejected", code)
		}
		if _, ok := h.clients[client]; ok {
			t.Fatal("rejected account link was published")
		}
	}
}
