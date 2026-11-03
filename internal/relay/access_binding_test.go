package relay

import (
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Reusing a sharing code must not resurrect a connection authenticated before
// rotation or device recreation; a new handshake with that code remains valid.
func TestAccess_should_requireFreshBinding_whenCredentialValueReused(t *testing.T) {
	for _, recreate := range []bool{false, true} {
		name := "grant-rotation"
		if recreate {
			name = "device-recreation"
		}
		t.Run(name, func(t *testing.T) {
			h := NewHub(newMemStore(), nil, nil)
			dev, stale := &recSender{}, &recSender{}
			if code := h.RegisterDevice("123456789", "initial", true, dev); code != "" {
				t.Fatal(code)
			}
			rotate := func(hash string) {
				h.Handle(dev, protocol.RoleDevice, "123456789", protocol.Message{Type: protocol.TypeDeviceSettings, RequestID: "settings", Inbound: protocol.Bool(true), GrantHash: protocol.SHA256Hex(hash)})
			}
			rotate("shared")
			if code := h.RegisterClient("123456789", "shared", "", stale); code != "" {
				t.Fatal(code)
			}
			if recreate {
				h.Handle(dev, protocol.RoleDevice, "123456789", protocol.Message{Type: protocol.TypeRevoke})
				dev = &recSender{}
				if code := h.RegisterDevice("123456789", "replacement", true, dev); code != "" {
					t.Fatal(code)
				}
			} else {
				rotate("temporary")
			}
			rotate("shared")
			h.Handle(stale, protocol.RoleClient, "123456789", protocol.Message{Type: protocol.TypeInvoke, Op: protocol.OpInfo, ExecID: "stale"})
			if stale.last().Code != protocol.Unauthorized || h.requests[requestKey{client: stale, id: "stale"}] != "" {
				t.Fatalf("stale connection regained access: %+v", stale.last())
			}
			fresh := &recSender{}
			if code := h.RegisterClient("123456789", "shared", "", fresh); code != "" {
				t.Fatal(code)
			}
			h.Handle(fresh, protocol.RoleClient, "123456789", protocol.Message{Type: protocol.TypeInvoke, Op: protocol.OpInfo, ExecID: "fresh"})
			if dev.last().Type != protocol.TypeInvoke || dev.last().ExecID != h.requests[requestKey{client: fresh, id: "fresh"}] {
				t.Fatal("new authenticated binding could not route")
			}
		})
	}
}
