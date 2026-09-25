package relay

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

func TestHelloDeviceStaysOpenAfterHandlerReturns(t *testing.T) {
	hub := NewHub(newMemStore())
	srv := httptest.NewServer(Handler(hub, nil))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+srv.URL[4:], nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close(websocket.StatusNormalClosure, "")

	err = wsjson.Write(ctx, c, protocol.Message{
		Type: protocol.TypeHelloDevice, DeviceID: "dev_t", Secret: "s",
		Inbound: protocol.Bool(false),
	})
	if err != nil {
		t.Fatal(err)
	}
	var ack protocol.Message
	if err := wsjson.Read(ctx, c, &ack); err != nil {
		t.Fatal(err)
	}
	if ack.Type != protocol.TypeHelloOK {
		t.Fatalf("hello: %+v", ack)
	}
	time.Sleep(200 * time.Millisecond)
	if err := wsjson.Write(ctx, c, protocol.Message{Type: protocol.TypeHeartbeat}); err != nil {
		t.Fatalf("conn died after handler return: %v", err)
	}
}
