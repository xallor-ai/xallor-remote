package noderun

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

type stalledWriteConn struct {
	net.Conn
	stall           atomic.Bool
	entered, resume chan struct{}
	once            sync.Once
}

func (c *stalledWriteConn) Write(data []byte) (int, error) {
	if c.stall.CompareAndSwap(true, false) {
		close(c.entered)
		<-c.resume
	}
	return c.Conn.Write(data)
}
func (c *stalledWriteConn) unblock() { c.once.Do(func() { close(c.resume) }) }

// Cancel an individual request after its frame write has started. The caller
// must return promptly while the connection owns that write; another cancelled
// waiter must not enqueue a frame, and later traffic must use the same socket.
func TestWSConnection_should_preserveSharedTransport_whenRequestCancelledDuringWrite(t *testing.T) {
	ctx, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	frames := make(chan protocol.Message, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		for {
			var m protocol.Message
			if wsjson.Read(ctx, c, &m) != nil {
				return
			}
			frames <- m
		}
	}))
	defer server.Close()
	var raw *stalledWriteConn
	transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		c, err := (&net.Dialer{}).DialContext(ctx, network, address)
		if err != nil {
			return nil, err
		}
		raw = &stalledWriteConn{Conn: c, entered: make(chan struct{}), resume: make(chan struct{})}
		return raw, nil
	}}
	defer transport.CloseIdleConnections()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), &websocket.DialOptions{HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	defer raw.unblock()
	link := &wsConn{ctx: ctx, conn: c}
	raw.stall.Store(true)
	request, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- link.sendContext(request, protocol.Message{Type: protocol.TypeExecutionRenewRoute, RequestID: "first"})
	}()
	select {
	case <-raw.entered:
	case <-ctx.Done():
		t.Fatal("write not entered")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Error("request cancellation waited for the shared write")
	}
	waiter, cancelWaiter := context.WithCancel(ctx)
	cancelWaiter()
	if err := link.sendContext(waiter, protocol.Message{Type: protocol.TypeHeartbeat, RequestID: "cancelled-waiter"}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	raw.unblock()
	if err := link.send(protocol.Message{Type: protocol.TypeHeartbeat, RequestID: "after"}); err != nil {
		t.Fatal("request cancellation killed shared transport", err)
	}
	for _, id := range []string{"first", "after"} {
		select {
		case m := <-frames:
			if m.RequestID != id {
				t.Fatal("unexpected frame", m)
			}
		case <-ctx.Done():
			t.Fatal("missing shared connection traffic")
		}
	}
}
