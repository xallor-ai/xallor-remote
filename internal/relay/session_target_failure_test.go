package relay

import (
	"errors"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

type targetRejectSender struct {
	*recSender
	kind string
}

func (s *targetRejectSender) Enqueue(m protocol.Message) error {
	if m.Type == s.kind {
		return errors.New("fixture queue refused")
	}
	return s.recSender.Enqueue(m)
}

// Early paths that clear conn or delete the slot cannot rely on a later Drop
// to invalidate persistent consent. Inspect SQLite before any later Drop.
func TestSessionTarget_shouldRetireConsent_whenRoutingStopsBeforeDrop(t *testing.T) {
	for _, failure := range []string{"invoke-queue", "settings-queue", "identity-write", "device-delete"} {
		t.Run(failure, func(t *testing.T) {
			s, h, target, client := approvedSessionFixture(t)
			active := client.last()
			var from Sender = target
			if failure == "invoke-queue" || failure == "settings-queue" {
				kind := protocol.TypeInvoke
				if failure == "settings-queue" {
					kind = protocol.TypeDeviceSettingsResult
				}
				from = &targetRejectSender{recSender: target, kind: kind}
				h.devices["device"].conn = from
				h.devices["device"].sessionConnection = from
			}
			if failure == "identity-write" {
				if _, err := s.faults.Exec(`CREATE TRIGGER deny_target_identity BEFORE UPDATE ON devices BEGIN SELECT RAISE(ABORT,'fixture refusal'); END;`); err != nil {
					t.Fatal(err)
				}
			}
			switch failure {
			case "invoke-queue":
				h.Handle(client, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeInvoke, ExecID: "refused", SessionID: active.SessionID, Op: protocol.OpInfo})
			case "device-delete":
				h.Handle(from, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeRevoke})
			default:
				h.Handle(from, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeDeviceSettings, RequestID: "settings", GrantHash: protocol.SHA256Hex("xr_grant_transport"), Inbound: protocol.Bool(true)})
			}
			if h.DeviceOnline("device") {
				t.Fatal("failed route stayed online")
			}
			assertRetiredTargetConsent(t, s, active)
		})
	}
}

// An unconfirmed persistent retirement still fences memory. A replacement
// cannot route until rotation succeeds; no failure restores old consent.
func TestSessionTarget_shouldKeepRoutingStopped_whenRetirementOrRotationFails(t *testing.T) {
	s, h, target, client := approvedSessionFixture(t)
	active := client.last()
	h.Drop(client, protocol.RoleClient, "device")
	if _, err := s.faults.Exec(`CREATE TRIGGER deny_target_retirement BEFORE UPDATE ON resume_capabilities BEGIN SELECT RAISE(ABORT,'fixture refusal'); END;`); err != nil {
		t.Fatal(err)
	}
	h.Drop(target, protocol.RoleDevice, "device")
	if h.DeviceOnline("device") {
		t.Fatal("failed retirement restored route")
	}
	if code := h.RegisterDevice("device", "secret", true, &recSender{}); code == "" {
		t.Fatal("failed rotation published replacement")
	}
	if h.DeviceOnline("device") {
		t.Fatal("replacement restored old route")
	}
	if _, err := s.faults.Exec(`DROP TRIGGER deny_target_retirement;`); err != nil {
		t.Fatal(err)
	}
	if code := h.RegisterDevice("device", "secret", true, &recSender{}); code != "" {
		t.Fatal(code)
	}
	assertRetiredTargetConsent(t, s, active)
}

// A greeting queue refusal after committed rotation must not publish the new
// connection or resurrect the old slot; consumed access codes stay consumed.
func TestSessionTarget_shouldKeepRoutingStopped_whenNewGreetingIsRefused(t *testing.T) {
	s, h, _, client := approvedSessionFixture(t)
	active := client.last()
	from := &targetRejectSender{recSender: &recSender{}, kind: protocol.TypeHelloOK}
	if code := h.RegisterDevice("device", "secret", true, from); code == "" {
		t.Fatal("refused greeting published")
	}
	if h.DeviceOnline("device") {
		t.Fatal("old slot restored")
	}
	assertRetiredTargetConsent(t, s, active)
}
