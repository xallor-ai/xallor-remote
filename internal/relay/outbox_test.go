package relay

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

func blockedOutbox(t *testing.T, blockType string) (*outbox, <-chan struct{}, chan struct{}, <-chan protocol.Message) {
	t.Helper()
	entered, release := make(chan struct{}), make(chan struct{})
	written := make(chan protocol.Message, protocol.MaxRelayQueueFrames+4)
	var once sync.Once
	b := newOutbox(context.Background(), func(ctx context.Context, m protocol.Message) error {
		if m.Type == blockType {
			once.Do(func() { close(entered) })
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		written <- m
		return nil
	}, func() {})
	t.Cleanup(func() { b.cancel(); b.shutdown() })
	return b, entered, release, written
}

func waitSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatal("fixture did not make progress")
	}
}

// A blocked real writer must neither block another device nor lose the flooded execution's terminal.
func TestOutbox_should_isolateSlowConsumer_and_preserveTruncatedExit(t *testing.T) {
	b, entered, release, written := blockedOutbox(t, protocol.TypeStdout)
	h := NewHub(nil)
	a, other := &recSender{}, &recSender{}
	if h.RegisterDevice("a", "secret-a", true, a) != "" || h.RegisterDevice("b", "secret-b", true, other) != "" {
		t.Fatal("register")
	}
	h.Handle(a, protocol.RoleDevice, "a", protocol.Message{Type: protocol.TypeGrantRotate, NewGrantHash: protocol.SHA256Hex("grant")})
	if h.RegisterClient("a", "grant", "", b) != "" {
		t.Fatal("client")
	}
	h.Handle(b, protocol.RoleClient, "a", protocol.Message{Type: protocol.TypeInvoke, ExecID: "exec", Op: protocol.OpExec})
	routeID := a.last().ExecID
	h.Handle(a, protocol.RoleDevice, "a", protocol.Message{Type: protocol.TypeStdout, ExecID: routeID, Data: "first"})
	waitSignal(t, entered)
	progress := make(chan struct{})
	go func() {
		if h.DeviceOnline("b") {
			close(progress)
		}
	}()
	waitSignal(t, progress)
	for i := 0; i < 40; i++ {
		h.Handle(a, protocol.RoleDevice, "a", protocol.Message{Type: protocol.TypeStdout, ExecID: routeID, Data: strings.Repeat("x", protocol.MaxFrameBytes)})
	}
	h.Handle(a, protocol.RoleDevice, "a", protocol.Message{Type: protocol.TypeExit, ExecID: routeID, ExitCode: protocol.Int(0), Status: protocol.ExitCompleted})
	if len(h.inflight) != 0 {
		t.Fatal("terminal did not release routing state")
	}
	b.mu.Lock()
	buffered, total := b.buffered["exec"], b.bytes
	b.mu.Unlock()
	if buffered > protocol.MaxRelayBufferBytes || total > protocol.MaxRelayQueueBytes {
		t.Fatal("queue exceeded byte budget")
	}
	close(release)
	deadline := time.After(2 * time.Second)
	for {
		select {
		case m := <-written:
			if m.Type == protocol.TypeExit {
				if m.Truncated == nil || !*m.Truncated {
					t.Fatal("lost queue truncation")
				}
				return
			}
		case <-deadline:
			t.Fatal("lost terminal")
		}
	}
}

// A full queue must make space for a terminal by evicting only that flow's output.
func TestOutbox_should_evictOwnOutput_whenTerminalNeedsSpace(t *testing.T) {
	b, entered, release, written := blockedOutbox(t, protocol.TypeHelloOK)
	if err := b.Enqueue(protocol.Message{Type: protocol.TypeHelloOK}); err != nil {
		t.Fatal(err)
	}
	waitSignal(t, entered)
	if err := b.Enqueue(protocol.Message{Type: protocol.TypeStdout, ExecID: "own", Data: "discard"}); err != nil {
		t.Fatal(err)
	}
	for i := 1; i < protocol.MaxRelayQueueFrames; i++ {
		if err := b.Enqueue(protocol.Message{Type: protocol.TypeHeartbeat}); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.Enqueue(protocol.Message{Type: protocol.TypeExit, ExecID: "own", ExitCode: protocol.Int(0)}); err != nil {
		t.Fatal(err)
	}
	close(release)
	b.shutdown()
	for len(written) > 0 {
		m := <-written
		if m.Type == protocol.TypeStdout {
			t.Fatal("evicted output was delivered")
		}
		if m.Type == protocol.TypeExit {
			if m.Truncated == nil || !*m.Truncated {
				t.Fatal("eviction not reported")
			}
			return
		}
	}
	t.Fatal("missing terminal")
}

// Control floods must close the bounded writer instead of allocating indefinitely.
func TestOutbox_should_close_whenControlQueueExhausted(t *testing.T) {
	b, entered, _, _ := blockedOutbox(t, protocol.TypeHelloOK)
	if err := b.Enqueue(protocol.Message{Type: protocol.TypeHelloOK}); err != nil {
		t.Fatal(err)
	}
	waitSignal(t, entered)
	for i := 0; i < protocol.MaxRelayQueueFrames; i++ {
		if err := b.Enqueue(protocol.Message{Type: protocol.TypeHeartbeat}); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.Enqueue(protocol.Message{Type: protocol.TypeHeartbeat}); err != errOutboxClosed {
		t.Fatalf("overflow: %v", err)
	}
	waitSignal(t, b.done)
	if err := b.Enqueue(protocol.Message{Type: protocol.TypeHeartbeat}); err != errOutboxClosed {
		t.Fatal("accepted after close")
	}
}

// An idle parent cancellation interrupts a stalled write and reclaims the writer goroutine.
func TestOutbox_should_stopWriter_whenContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	entered := make(chan struct{})
	b := newOutbox(ctx, func(writeCtx context.Context, m protocol.Message) error {
		deadline, ok := writeCtx.Deadline()
		if !ok || time.Until(deadline) > protocol.RelayWriteTimeout {
			t.Error("missing bounded write deadline")
		}
		close(entered)
		<-writeCtx.Done()
		return writeCtx.Err()
	}, func() {})
	t.Cleanup(func() { cancel(); b.shutdown() })
	if err := b.Enqueue(protocol.Message{Type: protocol.TypeHeartbeat}); err != nil {
		t.Fatal(err)
	}
	waitSignal(t, entered)
	cancel()
	waitSignal(t, b.done)
}

// Registering publishes hello before a subsequent routed invoke, with a single writer preserving FIFO.
func TestOutbox_should_greetBeforeInvoke_whenDeviceBecomesRoutable(t *testing.T) {
	written := make(chan protocol.Message, 4)
	b := newOutbox(context.Background(), func(_ context.Context, m protocol.Message) error { written <- m; return nil }, func() {})
	t.Cleanup(b.shutdown)
	h := NewHub(nil)
	if h.RegisterDevice("device", "secret", true, b) != "" {
		t.Fatal("register")
	}
	h.Handle(b, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeGrantRotate, NewGrantHash: protocol.SHA256Hex("grant")})
	client := &recSender{}
	if h.RegisterClient("device", "grant", "", client) != "" {
		t.Fatal("client")
	}
	h.Handle(client, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeInvoke, ExecID: "exec", Op: protocol.OpExec})
	b.shutdown()
	if first, second := <-written, <-written; first.Type != protocol.TypeHelloOK || second.Type != protocol.TypeInvoke {
		t.Fatalf("out of order: %+v %+v", first, second)
	}
}

// Non-output metadata also consumes the byte budget; a frame-count cap alone is insufficient.
func TestOutbox_should_boundMetadata_whenControlFramesAreLarge(t *testing.T) {
	b, entered, _, _ := blockedOutbox(t, protocol.TypeHelloOK)
	if err := b.Enqueue(protocol.Message{Type: protocol.TypeHelloOK}); err != nil {
		t.Fatal(err)
	}
	waitSignal(t, entered)
	m := protocol.Message{Type: protocol.TypeHeartbeat, Workspace: strings.Repeat("x", protocol.MaxRelayBufferBytes)}
	for i := 0; i <= protocol.MaxRelayQueueBytes/protocol.MaxRelayBufferBytes; i++ {
		err := b.Enqueue(m)
		if err == errOutboxClosed {
			waitSignal(t, b.done)
			return
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Fatal("metadata was not included in queue budget")
}

// A transport write failure closes the socket and discards retained buffers without retrying messages.
func TestOutbox_should_releaseBuffers_whenWriteFails(t *testing.T) {
	disconnected := make(chan struct{})
	b := newOutbox(context.Background(), func(context.Context, protocol.Message) error { return io.ErrClosedPipe }, func() { close(disconnected) })
	t.Cleanup(b.shutdown)
	if err := b.Enqueue(protocol.Message{Type: protocol.TypeStdout, ExecID: "exec", Data: "once"}); err != nil {
		t.Fatal(err)
	}
	waitSignal(t, disconnected)
	waitSignal(t, b.done)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.bytes != 0 || b.queue != nil || b.buffered != nil {
		t.Fatal("failed writer retained buffered data")
	}
}
