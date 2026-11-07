package relay

import (
	"context"
	"errors"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/admission"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// A storage outage must reject registration and client authentication, not act as an absent identity.
func TestIdentity_should_rejectRegistrationAndClient_whenStoreFails(t *testing.T) {
	s := openIdentityTestStore(t)
	h := NewHub(s, nil, nil, nil, admission.New(admission.Limits{PerDevice: protocol.MaxDeviceRequests}))
	dev := &recSender{}
	if code := h.RegisterDevice("existing", "original", true, dev); code != "" {
		t.Fatal(code)
	}
	h.Handle(dev, protocol.RoleDevice, "existing", protocol.Message{Type: protocol.TypeDeviceSettings, RequestID: "settings", Inbound: protocol.Bool(true), GrantHash: protocol.SHA256Hex("grant")})
	if _, err := s.faults.Exec(`DROP TABLE devices`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadDevice(context.Background(), "existing"); err == nil || errors.Is(err, access.ErrDeviceNotFound) {
		t.Fatalf("storage failure masked: %v", err)
	}
	if code := h.RegisterDevice("existing", "other", true, &recSender{}); code != protocol.RelayError {
		t.Fatalf("registration: %s", code)
	}
	if code := h.RegisterClient("existing", "grant", "", &recSender{}); code != protocol.RelayError {
		t.Fatalf("cached authorization bypassed outage: %s", code)
	}
	if h.devices["existing"].conn != dev {
		t.Fatal("failed registration replaced the existing connection")
	}
}

// Each failed authorization mutation must leave durable and live state unchanged.
func TestIdentity_should_preserveAuthorization_whenMutationFails(t *testing.T) {
	for _, kind := range []string{protocol.TypeDeviceSettings, protocol.TypeRevoke} {
		t.Run(kind, func(t *testing.T) {
			s := openIdentityTestStore(t)
			h := NewHub(s, nil, nil, nil, admission.New(admission.Limits{PerDevice: protocol.MaxDeviceRequests}))
			dev := &recSender{}
			if code := h.RegisterDevice("device", "secret", true, dev); code != "" {
				t.Fatal(code)
			}
			h.Handle(dev, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeDeviceSettings, RequestID: "settings", Inbound: protocol.Bool(true), GrantHash: protocol.SHA256Hex("original")})
			before, err := s.LoadDevice(context.Background(), "device")
			if err != nil {
				t.Fatal(err)
			}
			statement := `CREATE TRIGGER reject_write BEFORE UPDATE ON devices BEGIN SELECT RAISE(ABORT, 'fixture write failure'); END;`
			if kind == protocol.TypeRevoke {
				statement = `CREATE TRIGGER reject_write BEFORE DELETE ON devices BEGIN SELECT RAISE(ABORT, 'fixture delete failure'); END;`
			}
			if _, err := s.faults.Exec(statement); err != nil {
				t.Fatal(err)
			}
			h.Handle(dev, protocol.RoleDevice, "device", protocol.Message{Type: kind, RequestID: "settings", GrantHash: protocol.SHA256Hex("replacement"), Inbound: protocol.Bool(false)})
			if dev.last().Code != protocol.RelayError {
				t.Fatalf("mutation failure not reported: %+v", dev.last())
			}
			if h.DeviceOnline("device") {
				t.Fatal("failed authorization remained routable during close")
			}
			after, err := s.LoadDevice(context.Background(), "device")
			if err != nil || before != after || h.access["device"] != before.Binding() || !h.devices["device"].record.Inbound {
				t.Fatalf("failed mutation changed identity: %+v %v", after, err)
			}
		})
	}
}
