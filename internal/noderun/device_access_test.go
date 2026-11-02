package noderun

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Responses are bound to the exact live device link, and cleaned-up requests
// cannot be completed by a late or duplicate response from another connection.
func TestDeviceAccess_should_ignoreWrongLinkAndLateResponse(t *testing.T) {
	var requests accessRequests
	link := &wsConn{}
	id, result, remove, err := requests.register(link)
	if err != nil {
		t.Fatal(err)
	}
	requests.deliver(&wsConn{}, protocol.Message{RequestID: id, Code: "wrong"})
	select {
	case <-result:
		t.Fatal("wrong connection completed request")
	default:
	}
	requests.deliver(link, protocol.Message{RequestID: id, Code: "first"})
	requests.deliver(link, protocol.Message{RequestID: id, Code: "duplicate"})
	if (<-result).Code != "first" {
		t.Fatal("duplicate replaced response")
	}
	remove()
	requests.deliver(link, protocol.Message{RequestID: id})
	select {
	case <-result:
		t.Fatal("late response retained")
	default:
	}
	if len(requests.pending) != 0 {
		t.Fatal("request leaked")
	}
}

// Pending management requests have a hard admission cap; cancellation releases
// one slot without permitting duplicate responses to retain the old waiter.
func TestDeviceAccess_should_releaseCapacity_whenWaiterRemoved(t *testing.T) {
	var requests accessRequests
	var cleanup []func()
	for range 16 {
		_, _, remove, err := requests.register(&wsConn{})
		if err != nil {
			t.Fatal(err)
		}
		cleanup = append(cleanup, remove)
	}
	if _, _, _, err := requests.register(&wsConn{}); err == nil {
		t.Fatal("unbounded pending management")
	}
	cleanup[0]()
	_, _, remove, err := requests.register(&wsConn{})
	if err != nil {
		t.Fatal(err)
	}
	remove()
	for _, remove := range cleanup {
		remove()
	}
	if len(requests.pending) != 0 {
		t.Fatal("capacity not reclaimed")
	}
}

// A management deadline must cancel a wait for the writer, without needing the
// unrelated write to finish or touching a websocket that was never acquired.
func TestWSConn_should_cancelWriteAdmission_whenWriterBusy(t *testing.T) {
	w := &wsConn{}
	w.gateOnce.Do(func() { w.writeGate = make(chan struct{}, 1) })
	w.writeGate <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- w.sendContext(ctx, protocol.Message{Type: protocol.TypeAccessRequest}) }()
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("writer admission ignored cancellation")
	}
}
