package relay

import (
	"github.com/xallor-ai/xallor-remote/internal/protocol"
	"testing"
)

func authorizeTestClient(t *testing.T, h *Hub, dev, client Sender) {
	t.Helper()
	h.Handle(dev, protocol.RoleDevice, "dev_a", protocol.Message{Type: protocol.TypeGrantRotate, NewGrantHash: protocol.SHA256Hex("test")})
	if code := h.RegisterClient("dev_a", "test", client); code != "" {
		t.Fatal(code)
	}
}
