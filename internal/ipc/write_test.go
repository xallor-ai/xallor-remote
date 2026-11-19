package ipc

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"
)

type startedWriteConn struct {
	net.Conn
	once    sync.Once
	started chan struct{}
}

func (c *startedWriteConn) Write(b []byte) (int, error) {
	c.once.Do(func() { close(c.started) })
	return c.Conn.Write(b)
}

// A queued bounded frame must time out even while a prior unbounded writer owns the gate.
func TestWriteFrameContextShouldBoundQueueWait(t *testing.T) {
	server, head := net.Pipe()
	defer server.Close()
	defer head.Close()
	raw := &startedWriteConn{Conn: server, started: make(chan struct{})}
	c := NewConn(raw)
	first := make(chan error, 1)
	go func() { first <- c.WriteFrame(Event("first", "notice", "")) }()
	<-raw.started
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	second := make(chan error, 1)
	go func() { second <- c.WriteFrameContext(ctx, Event("second", "notice", "")) }()
	select {
	case err := <-second:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		server.Close()
		<-first
		t.Fatal("queued writer ignored its deadline")
	}
	frame, err := NewConn(head).ReadFrame()
	if err != nil || frame.ID != "first" {
		t.Fatalf("%+v %v", frame, err)
	}
	if err := <-first; err != nil {
		t.Fatal(err)
	}
}

// Cancellation interrupts the current write, and its callback/deadline cannot poison the next frame.
func TestWriteFrameContextShouldCancelAndLeaveNextWriteUsable(t *testing.T) {
	server, head := net.Pipe()
	defer server.Close()
	defer head.Close()
	raw := &startedWriteConn{Conn: server, started: make(chan struct{})}
	c := NewConn(raw)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- c.WriteFrameContext(ctx, Event("cancelled", "notice", "")) }()
	<-raw.started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		server.Close()
		<-done
		t.Fatal("write did not observe cancellation")
	}
	next, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	go func() { done <- c.WriteFrameContext(next, Event("next", "notice", "")) }()
	frame, err := NewConn(head).ReadFrame()
	if err != nil || frame.ID != "next" {
		t.Fatalf("%+v %v", frame, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
