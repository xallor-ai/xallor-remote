package relay

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// A real SQL pool stall fills the receive queue. Dropped output must still consume
// traffic quota, and the quota error must flush before cancelling the blocked writer.
func TestInboxWire_should_chargeDroppedOutput_whenIdentityHandlerBlocked(t *testing.T) {
	s := openIdentityTestStore(t)
	h := NewHub(s, nil, nil)
	output := protocol.Message{Type: protocol.TypeStdout, ExecID: "flood", Data: strings.Repeat("x", 512)}
	raw, err := output.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	lim := NewLimiter(Quota{Enabled: true, BytesPerMinute: int64(len(raw) * 70)})
	srv := httptest.NewTLSServer(HandlerQuota(h, nil, lim))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "wss"+srv.URL[5:], &websocket.DialOptions{HTTPClient: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	if err := wsjson.Write(ctx, c, protocol.Message{Type: protocol.TypeHelloDevice, DeviceID: "device", Secret: "secret"}); err != nil {
		t.Fatal(err)
	}
	var reply protocol.Message
	if err := wsjson.Read(ctx, c, &reply); err != nil || reply.Type != protocol.TypeHelloOK {
		t.Fatalf("hello: %+v %v", reply, err)
	}
	held, err := s.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	before := s.db.Stats().WaitCount
	if err := wsjson.Write(ctx, c, protocol.Message{Type: protocol.TypeDeviceSettings, RequestID: "settings", Inbound: protocol.Bool(true), GrantHash: protocol.SHA256Hex("pending")}); err != nil {
		t.Fatal(err)
	}
	waitCondition(t, func() bool { return s.db.Stats().WaitCount > before })
	fast, stop := context.WithTimeout(ctx, 2*time.Second)
	defer stop()
	for range 70 {
		if err := wsjson.Write(fast, c, output); err != nil {
			t.Fatal(err)
		}
	}
	if err := wsjson.Read(fast, c, &reply); err != nil || reply.Type != protocol.TypeError || reply.Code != protocol.QuotaExceeded {
		t.Fatalf("dropped output bypassed quota or quota response was lost: %+v %v", reply, err)
	}
	waitCondition(t, func() bool { return !h.DeviceOnline("device") })
	if err := held.Close(); err != nil {
		t.Fatal(err)
	}
	record, err := s.LoadDevice(ctx, "device")
	if err != nil || record.GrantHash != "" {
		t.Fatalf("cancelled authorization committed: %+v %v", record, err)
	}
}
