package noderun

import (
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Missing resource context is a failed handshake, not an old-client fallback.
// A settings acknowledgement alone must never publish the Runtime as online.
func TestDeviceContext_should_requireAuthoritativeSnapshot_beforeOnline(t *testing.T) {
	f := newSettingsWire(t)
	connected := make(chan error, 1)
	go func() { connected <- f.d.connectDevice() }()
	f.next(t, protocol.TypeHelloDevice)
	f.replies <- protocol.Message{Type: protocol.TypeHelloOK, Role: protocol.RoleDevice}
	settings := f.next(t, protocol.TypeDeviceSettings)
	f.replies <- protocol.Message{Type: protocol.TypeDeviceSettingsResult, RequestID: settings.RequestID}
	select {
	case err := <-connected:
		if err == nil || f.d.deviceOnline() {
			t.Fatal("missing context went online")
		}
	case <-f.d.ctx.Done():
		t.Fatal("handshake did not reject missing context")
	}
}
