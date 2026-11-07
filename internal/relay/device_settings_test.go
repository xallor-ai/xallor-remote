package relay

import (
	"context"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/admission"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

type settingsCommitGate struct {
	DeviceStore
	entered, release chan struct{}
}

func (s *settingsCommitGate) UpdateDevice(ctx context.Context, id string, r access.Device) (access.Device, error) {
	close(s.entered)
	select {
	case <-s.release:
	case <-ctx.Done():
		return access.Device{}, ctx.Err()
	}
	return s.DeviceStore.UpdateDevice(ctx, id, r)
}

// Registration authenticates a connection but cannot route from stale saved
// settings. Neither a blocked write nor hello is a settings confirmation.
func TestDeviceSettings_should_confirmAndRouteOnlyAfterCommit(t *testing.T) {
	s := &settingsCommitGate{DeviceStore: newMemStore(), entered: make(chan struct{}), release: make(chan struct{})}
	h := NewHub(s, nil, nil, nil, admission.New(admission.Limits{PerDevice: protocol.MaxDeviceRequests}))
	d := &recSender{}
	if code := h.RegisterDevice("device", "secret", false, d); code != "" {
		t.Fatal(code)
	}
	finished := make(chan struct{})
	go func() {
		h.Handle(d, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeDeviceSettings, RequestID: "change", GrantHash: protocol.SHA256Hex("grant"), Inbound: protocol.Bool(true)})
		close(finished)
	}()
	waitSignal(t, s.entered)
	if d.last().Type != protocol.TypeHelloOK || h.devices["device"].ready || h.devices["device"].record.Inbound {
		t.Fatal("settings visible before commit")
	}
	close(s.release)
	waitSignal(t, finished)
	if m := d.last(); m.Type != protocol.TypeDeviceSettingsResult || m.RequestID != "change" || m.Code != "" {
		t.Fatalf("missing confirmation: %+v", m)
	}
	client := &recSender{}
	if code := h.RegisterClient("device", "grant", "", client); code != "" {
		t.Fatal(code)
	}
	h.Handle(client, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeInvoke, ExecID: "exec", Op: protocol.OpInfo})
	if d.last().Type != protocol.TypeInvoke {
		t.Fatal("confirmed device cannot route")
	}
}

// A failed grant-table write occurs after the inbound UPDATE within the same
// real SQLite transaction. Both fields must roll back and no success may escape.
func TestDeviceSettings_should_rollbackBothFields_whenGrantWriteFails(t *testing.T) {
	s := openIdentityTestStore(t)
	h := NewHub(s, nil, nil, nil, admission.New(admission.Limits{PerDevice: protocol.MaxDeviceRequests}))
	d := &recSender{}
	if code := h.RegisterDevice("device", "secret", false, d); code != "" {
		t.Fatal(code)
	}
	before, err := s.LoadDevice(context.Background(), "device")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.faults.Exec(`CREATE TRIGGER reject_grant BEFORE INSERT ON device_grants BEGIN SELECT RAISE(ABORT, 'fixture grant failure'); END;`); err != nil {
		t.Fatal(err)
	}
	h.Handle(d, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeDeviceSettings, RequestID: "change", GrantHash: protocol.SHA256Hex("grant"), Inbound: protocol.Bool(true)})
	after, err := s.LoadDevice(context.Background(), "device")
	if err != nil || after != before || d.last().Code != protocol.RelayError || h.DeviceOnline("device") {
		t.Fatal("partial update or false success", err)
	}
}

// Reconnecting with an old enabled record cannot execute until this connection
// confirms its own complete settings; malformed and client-originated updates
// cannot activate it.
func TestDeviceSettings_should_requireNewSnapshot_whenDeviceReconnects(t *testing.T) {
	h, old, client, _ := routingFixture(t)
	fresh := &recSender{}
	if code := h.RegisterDevice("device", "secret", true, fresh); code != "" {
		t.Fatal(code)
	}
	h.Handle(client, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeInvoke, ExecID: "early", Op: protocol.OpInfo})
	if client.last().Code != protocol.DeviceOffline {
		t.Fatal("routed before snapshot")
	}
	settings := protocol.Message{Type: protocol.TypeDeviceSettings, RequestID: "ready", GrantHash: protocol.SHA256Hex("grant"), Inbound: protocol.Bool(true)}
	h.Handle(old, protocol.RoleDevice, "device", settings)
	h.Handle(client, protocol.RoleClient, "device", settings)
	if h.devices["device"].ready {
		t.Fatal("unbound sender activated device")
	}
	invalid := settings
	invalid.Inbound = nil
	h.Handle(fresh, protocol.RoleDevice, "device", invalid)
	if fresh.last().Code != protocol.AgentError || h.devices["device"].ready {
		t.Fatal("partial settings accepted")
	}
	h.Handle(fresh, protocol.RoleDevice, "device", settings)
	h.Handle(client, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeInvoke, ExecID: "ready", Op: protocol.OpInfo})
	if fresh.last().Type != protocol.TypeInvoke {
		t.Fatal("confirmed snapshot did not enable route")
	}
}

// The real endpoint must not treat hello metadata as another access-setting
// source. A new identity stays disabled until one complete snapshot is applied.
func TestDeviceSettingsWire_should_disableNewIdentity_untilCompleteSnapshot(t *testing.T) {
	s := openIdentityTestStore(t)
	h := NewHub(s, nil, nil, nil, admission.New(admission.Limits{PerDevice: protocol.MaxDeviceRequests}))
	ctx, conn, hello := identityWire(t, h, protocol.Message{Type: protocol.TypeHelloDevice, DeviceID: "device", Secret: "secret", Inbound: protocol.Bool(true)})
	if hello.Type != protocol.TypeHelloOK {
		t.Fatal(hello.Code)
	}
	record, err := s.LoadDevice(ctx, "device")
	if err != nil || record.Inbound || record.GrantHash != "" {
		t.Fatal("hello applied access settings", err)
	}
	confirmWireSettings(t, ctx, conn, "grant", true)
	record, err = s.LoadDevice(ctx, "device")
	if err != nil || !record.Inbound || record.GrantHash != protocol.SHA256Hex("grant") {
		t.Fatal("snapshot missing after confirmation", err)
	}
}
