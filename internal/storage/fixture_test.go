package storage

import (
	"context"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

func openIdentityTestStore(t *testing.T) *SQLStore {
	t.Helper()
	s, err := OpenSQLStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}
func setTestAccess(t *testing.T, s *SQLStore, id, grant string, inbound bool) access.Device {
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
func waitCondition(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("fixture condition not reached")
		}
		time.Sleep(time.Millisecond)
	}
}
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
