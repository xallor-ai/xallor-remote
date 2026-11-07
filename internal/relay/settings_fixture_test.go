package relay

import (
	"context"
	"testing"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

func confirmWireSettings(t *testing.T, ctx context.Context, c *websocket.Conn, grant string, inbound bool) {
	t.Helper()
	hash := ""
	if grant != "" {
		hash = protocol.SHA256Hex(grant)
	}
	if err := wsjson.Write(ctx, c, protocol.Message{Type: protocol.TypeDeviceSettings, RequestID: "settings", GrantHash: hash, Inbound: protocol.Bool(inbound)}); err != nil {
		t.Fatal(err)
	}
	var ack protocol.Message
	if err := wsjson.Read(ctx, c, &ack); err != nil || ack.Type != protocol.TypeDeviceSettingsResult || ack.RequestID != "settings" || ack.Code != "" {
		t.Fatalf("settings: %+v %v", ack, err)
	}
}
