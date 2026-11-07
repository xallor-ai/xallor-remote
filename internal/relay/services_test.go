package relay

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/account"
	"github.com/xallor-ai/xallor-remote/internal/admission"
	"github.com/xallor-ai/xallor-remote/internal/principal"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Separate adapters expose only their own contract. Claim, authentication and
// per-invoke revocation must work without discovering services on DeviceStore.
func TestHub_should_useInjectedServices_whenDeviceStoreExposesOnlyDevices(t *testing.T) {
	s := openIdentityTestStore(t)
	a := enrollment(t, s, "owner", "")
	h := NewHub(struct{ DeviceStore }{s}, struct{ principal.Authenticator }{s}, struct{ DeviceAccess }{access.NewService(s)}, nil, admission.New(admission.Limits{PerDevice: protocol.MaxDeviceRequests}))
	device, client := &recSender{}, &recSender{}
	if code := h.RegisterDevice("device", "secret", true, device); code != "" {
		t.Fatal(code)
	}
	h.Handle(device, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeDeviceSettings, RequestID: "settings", Inbound: protocol.Bool(true), GrantHash: protocol.SHA256Hex("grant")})
	controlAccess(t, h, device, a.Token, access.Request{Action: "claim"})
	if code := h.RegisterClient("device", "", a.Token, client); code != "" {
		t.Fatal("injected authentication or authorization was bypassed", code)
	}
	h.Handle(client, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeInvoke, Op: protocol.OpInfo, ExecID: "before"})
	if h.requests[requestKey{client: client, id: "before"}] == "" {
		t.Fatal("owner's authorized request did not route")
	}
	if err := account.New(s).RevokeCredential(context.Background(), a.CredentialID); err != nil {
		t.Fatal(err)
	}
	h.Handle(client, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeInvoke, Op: protocol.OpInfo, ExecID: "after"})
	if client.last().Code != protocol.Unauthorized || h.requests[requestKey{client: client, id: "after"}] != "" {
		t.Fatal("revoked caller bypassed injected per-invoke authorization")
	}
}

// relaySQLFixture has incidental account/access methods, but omitted services must
// never enable account hello, device management or an owned-device downgrade.
func TestHub_should_denyAccountOperations_whenRequiredServiceIsAbsent(t *testing.T) {
	for _, missing := range []string{"accounts", "access", "both"} {
		t.Run(missing, func(t *testing.T) {
			s := openIdentityTestStore(t)
			a := enrollment(t, s, "owner", "")
			var auth principal.Authenticator = s
			var deviceAccess DeviceAccess = access.NewService(s)
			if missing != "access" {
				auth = nil
			}
			if missing != "accounts" {
				deviceAccess = nil
			}
			h := NewHub(s, auth, deviceAccess, nil, admission.New(admission.Limits{PerDevice: protocol.MaxDeviceRequests}))
			device := &recSender{}
			if code := h.RegisterDevice("device", "secret", true, device); code != "" {
				t.Fatal(code)
			}
			h.Handle(device, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeDeviceSettings, RequestID: "settings", Inbound: protocol.Bool(true), GrantHash: protocol.SHA256Hex("grant")})
			if code := h.RegisterClient("device", "grant", "", &recSender{}); code != "" {
				t.Fatal("unclaimed bearer access unnecessarily requires account services", code)
			}
			if auth == nil {
				if code := h.RegisterClient("device", "grant", a.Token, &recSender{}); code != protocol.Unauthorized {
					t.Fatal("account token silently downgraded to bearer", code)
				}
			}
			h.Handle(device, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeAccessRequest, RequestID: "claim", AccountToken: a.Token, Payload: json.RawMessage(`{"action":"claim"}`)})
			if device.last().Code != protocol.Unauthorized {
				t.Fatal("management discovered an omitted service")
			}
			record, err := s.LoadDevice(context.Background(), "device")
			if err != nil || record.OwnershipID != "" {
				t.Fatal("denied management changed ownership", err)
			}
			if _, err := access.NewService(s).Manage(context.Background(), "device", record, a.Identity, access.Request{Action: "claim"}); err != nil {
				t.Fatal(err)
			}
			// A fresh connection must load the committed ownership from storage.
			for _, token := range []string{"", a.Token} {
				if code := h.RegisterClient("device", "grant", token, &recSender{}); code != protocol.Unauthorized {
					t.Fatal("owned device bypassed a missing service", code)
				}
			}
		})
	}
}
