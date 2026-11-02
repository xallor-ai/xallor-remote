package relay

import (
	"context"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

func setTestAccess(t *testing.T, s DeviceStore, id, grant string, inbound bool) access.Device {
	t.Helper()
	r, err := s.LoadDevice(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	r.GrantHash, r.Inbound = grant, inbound
	r, err = s.UpdateDevice(context.Background(), id, r)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func authorizeTestClient(t *testing.T, h *Hub, dev, client Sender) {
	t.Helper()
	h.Handle(dev, protocol.RoleDevice, "dev_a", protocol.Message{Type: protocol.TypeGrantRotate, NewGrantHash: protocol.SHA256Hex("test")})
	if code := h.RegisterClient("dev_a", "test", "", client); code != "" {
		t.Fatal(code)
	}
}
