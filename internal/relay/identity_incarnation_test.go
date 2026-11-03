package relay

import (
	"context"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/admission"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// A freshly authenticated binding must not route to an older live incarnation
// when storage contains a different device with the same display number.
func TestAccess_should_requireMatchingDeviceUID_whenRoutingSnapshotIsOld(t *testing.T) {
	s := openIdentityTestStore(t)
	h := NewHub(s, nil, nil, admission.New(admission.Limits{PerDevice: protocol.MaxDeviceRequests}))
	device := &recSender{}
	if code := h.RegisterDevice("number", "secret", true, device); code != "" {
		t.Fatal(code)
	}
	old, _ := s.LoadDevice(context.Background(), "number")
	if err := s.DeleteDevice(context.Background(), "number", old.UID, old.SecretHash); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RegisterDevice(context.Background(), "number", "secret", true); err != nil {
		t.Fatal(err)
	}
	setTestAccess(t, s, "number", protocol.SHA256Hex("new-grant"), true)
	client := &recSender{}
	if code := h.RegisterClient("number", "new-grant", "", client); code != "" {
		t.Fatal(code)
	}
	h.Handle(client, protocol.RoleClient, "number", protocol.Message{Type: protocol.TypeInvoke, Op: protocol.OpInfo, ExecID: "new-identity"})
	if client.last().Code != protocol.DeviceOffline || h.requests[requestKey{client: client, id: "new-identity"}] != "" {
		t.Fatal("new identity routed into old incarnation")
	}
}
