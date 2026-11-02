package relay

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

type smallSendBufferListener struct{ net.Listener }

func (l smallSendBufferListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	if tcp, ok := c.(*net.TCPConn); ok {
		if err := tcp.SetWriteBuffer(1024); err != nil {
			c.Close()
			return nil, err
		}
	}
	return c, nil
}

// A real unread socket must not backpressure the target's shared device connection:
// another client on that same target still receives its complete terminal result.
func TestFlowWire_should_routeOtherClient_whenSocketConsumerStopsReading(t *testing.T) {
	s := openIdentityTestStore(t)
	secret, grant := protocol.SHA256Hex("secret"), protocol.SHA256Hex("grant")
	if _, err := s.RegisterDevice("device", secret, true); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateDevice("device", DeviceRecord{SecretHash: secret, GrantHash: grant, Inbound: true}); err != nil {
		t.Fatal(err)
	}
	h := NewHub(s)
	// Bound both TCP buffers so the fixture does not depend on OS autotuning
	// or on JSON parsing being faster than a socket writer.
	srv := httptest.NewUnstartedServer(Handler(h, nil))
	srv.Listener = smallSendBufferListener{srv.Listener}
	srv.Start()
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		c, err := (&net.Dialer{}).DialContext(ctx, network, address)
		if err != nil {
			return nil, err
		}
		if tcp, ok := c.(*net.TCPConn); ok {
			if err := tcp.SetReadBuffer(1024); err != nil {
				c.Close()
				return nil, err
			}
		}
		return c, nil
	}}
	defer transport.CloseIdleConnections()
	dial := func(hello protocol.Message, options *websocket.DialOptions) *websocket.Conn {
		t.Helper()
		c, _, err := websocket.Dial(ctx, "ws"+srv.URL[4:], options)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.CloseNow() })
		if err := wsjson.Write(ctx, c, hello); err != nil {
			t.Fatal(err)
		}
		var ack protocol.Message
		if err := wsjson.Read(ctx, c, &ack); err != nil || ack.Type != protocol.TypeHelloOK {
			t.Fatalf("hello: %+v %v", ack, err)
		}
		return c
	}
	device := dial(protocol.Message{Type: protocol.TypeHelloDevice, DeviceID: "device", Secret: "secret"}, nil)
	slow := dial(protocol.Message{Type: protocol.TypeHelloClient, TargetID: "device", Grant: "grant"}, &websocket.DialOptions{HTTPClient: &http.Client{Transport: transport}})
	healthy := dial(protocol.Message{Type: protocol.TypeHelloClient, TargetID: "device", Grant: "grant"}, nil)
	if err := wsjson.Write(ctx, slow, protocol.Message{Type: protocol.TypeInvoke, ExecID: "flood", Op: protocol.OpExec}); err != nil {
		t.Fatal(err)
	}
	var invoke protocol.Message
	if err := wsjson.Read(ctx, device, &invoke); err != nil || invoke.ExecID != "flood" {
		t.Fatalf("invoke: %+v %v", invoke, err)
	}
	chunk := strings.Repeat("x", protocol.MaxFrameBytes)
	for i := 0; i < 48; i++ {
		if err := wsjson.Write(ctx, device, protocol.Message{Type: protocol.TypeStdout, ExecID: "flood", Data: chunk}); err != nil {
			t.Fatal(err)
		}
	}
	// The short deadline detects connection-wide stalls; the slow writer's own
	// deadline is longer and must not be what permits this independent request.
	fastCtx, fastCancel := context.WithTimeout(ctx, 3*time.Second)
	defer fastCancel()
	if err := wsjson.Write(fastCtx, healthy, protocol.Message{Type: protocol.TypeInvoke, ExecID: "healthy", Op: protocol.OpInfo}); err != nil {
		t.Fatal(err)
	}
	if err := wsjson.Read(fastCtx, device, &invoke); err != nil || invoke.ExecID != "healthy" {
		t.Fatalf("shared device stalled: %+v %v", invoke, err)
	}
	if err := wsjson.Write(fastCtx, device, protocol.Message{Type: protocol.TypeExit, ExecID: "healthy", ExitCode: protocol.Int(0), Status: protocol.ExitCompleted}); err != nil {
		t.Fatal(err)
	}
	var terminal protocol.Message
	if err := wsjson.Read(fastCtx, healthy, &terminal); err != nil || terminal.Type != protocol.TypeExit || terminal.ExecID != "healthy" {
		t.Fatalf("healthy client stalled: %+v %v", terminal, err)
	}
	h.mu.Lock()
	inf := h.inflight["flood"]
	truncated := inf != nil && inf.trunc
	h.mu.Unlock()
	if !truncated {
		t.Fatal("fixture failed to exhaust the slow socket's output buffer")
	}
}
