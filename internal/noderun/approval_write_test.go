package noderun

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

type approvalObservedConn struct {
	net.Conn
	first   sync.Once
	started chan struct{}
	overlap chan struct{}
	writing atomic.Bool
}

func (c *approvalObservedConn) Write(data []byte) (int, error) {
	c.writing.Store(true)
	defer c.writing.Store(false)
	c.first.Do(func() { close(c.started) })
	return c.Conn.Write(data)
}
func (c *approvalObservedConn) SetWriteDeadline(deadline time.Time) error {
	if c.writing.Load() && deadline.After(time.Now().Add(250*time.Millisecond)) {
		select {
		case c.overlap <- struct{}{}:
		default:
		}
	}
	return c.Conn.SetWriteDeadline(deadline)
}

// Cancellation must interrupt a real blocked event write for both approval hubs,
// rather than waiting for the head to read or for the per-write deadline.
func TestApprovalWriteShouldCancelBlockedDelivery(t *testing.T) {
	for _, kind := range []string{"exec", "session"} {
		t.Run(kind, func(t *testing.T) {
			server, head := net.Pipe()
			raw := &approvalObservedConn{Conn: server, started: make(chan struct{}), overlap: make(chan struct{}, 1)}
			conn := ipc.NewConn(raw)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var ask func(context.Context) (bool, string)
			var pending func() int
			if kind == "exec" {
				h := newApprovalHub()
				h.subscribe(conn)
				ask = func(ctx context.Context) (bool, string) {
					return h.ask(ctx, testApprovalRequest("exec-cancel", "echo"))
				}
				pending = func() int { return len(h.list()) }
			} else {
				h := newSessionApprovalHub()
				h.subscribe(conn)
				ask = func(ctx context.Context) (bool, string) { return h.ask(ctx, sessionApprovalFixture()) }
				pending = func() int { return len(h.list()) }
			}
			done := make(chan string, 1)
			finished := false
			defer func() {
				server.Close()
				head.Close()
				if !finished {
					select {
					case <-done:
					case <-time.After(time.Second):
						t.Error("approval goroutine remained live")
					}
				}
			}()
			go func() {
				allow, code := ask(ctx)
				if allow {
					code = "unexpected allow"
				}
				done <- code
			}()
			select {
			case <-raw.started:
			case <-time.After(time.Second):
				t.Fatal("event did not reach transport")
			}
			cancel()
			select {
			case code := <-done:
				finished = true
				if code != protocol.Cancelled {
					t.Fatal(code)
				}
			case <-time.After(time.Second):
				t.Fatal("cancelled approval remained blocked in transport")
			}
			if pending() != 0 {
				t.Fatal("cancelled approval remains pending")
			}
		})
	}
}

// A second approval may queue, but cannot alter the deadline of the current write.
func TestApprovalWritesShouldNotChangeAnotherDeliveryDeadline(t *testing.T) {
	server, head := net.Pipe()
	defer server.Close()
	defer head.Close()
	raw := &approvalObservedConn{Conn: server, started: make(chan struct{}), overlap: make(chan struct{}, 1)}
	h := newApprovalHub()
	h.subscribe(ipc.NewConn(raw))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{}, 2)
	launched := 1
	defer func() {
		cancel()
		server.Close()
		head.Close()
		for i := 0; i < launched; i++ {
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Error("delivery did not settle")
			}
		}
	}()
	go func() { h.ask(ctx, testApprovalRequest("a", "echo a")); done <- struct{}{} }()
	select {
	case <-raw.started:
	case <-time.After(time.Second):
		t.Fatal("first delivery absent")
	}
	go func() { h.ask(ctx, testApprovalRequest("b", "echo b")); done <- struct{}{} }()
	launched++
	select {
	case <-raw.overlap:
		t.Fatal("second delivery changed the active writer deadline")
	case <-time.After(300 * time.Millisecond):
	}
}
