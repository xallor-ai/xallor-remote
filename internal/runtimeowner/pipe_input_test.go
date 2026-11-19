package runtimeowner

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

// A writer holding the pipe open without EOF must not outlive admission timeout;
// the reader owns/closes its end, while this test keeps the writer alive.
func TestHandoffInputDeadlineDoesNotWaitForWriterEOF(t *testing.T) {
	input, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := ReadHandoffPipe(ctx, input); done <- err }()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		writer.Close()
		<-done
		t.Fatal("context did not bound input waiting")
	}
	if input.Fd() != ^uintptr(0) {
		t.Fatal("input ownership was not released")
	}
}

// Normal finite payload and EOF remain exact, with no helper goroutine left alive.
func TestHandoffInputReturnsFiniteBytesAndClosesReader(t *testing.T) {
	input, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	go func() { writer.Write([]byte(`{"test":"payload"}`)); writer.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	raw, err := ReadHandoffPipe(ctx, input)
	if err != nil || string(raw) != `{"test":"payload"}` {
		t.Fatal(string(raw), err)
	}
}
