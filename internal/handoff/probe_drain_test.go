package handoff

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"
)

// The child has closed its writer, but the scheduled reader has not consumed
// the pipe yet. Successful completion must preserve all buffered Unicode bytes.
func TestProbe_should_drainBufferedOutput_whenProcessAlreadyExited(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	want := []byte("complete buffered output 中文")
	if _, err := writer.Write(want); err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()
	var got bytes.Buffer
	done := make(chan error, 1)
	go func() {
		time.Sleep(25 * time.Millisecond)
		_, err := io.Copy(&got, reader)
		done <- err
	}()
	if err := finishProbeRead(context.Background(), reader, done); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Bytes(), want) {
		t.Fatalf("lost buffered output: %q", got.Bytes())
	}
}

// A cancelled probe with a still-open inherited writer must release the reader
// and report cancellation rather than silently returning partial success.
func TestProbe_should_closeReader_whenCancelledBeforeEOF(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	done := make(chan error, 1)
	go func() { _, err := io.Copy(io.Discard, reader); done <- err }()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := finishProbeRead(ctx, reader, done); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

// An inherited writer may outlive the supervised process. Drain must terminate
// with a timeout error, release its reader, and join the copying goroutine.
func TestProbe_should_boundDrain_whenInheritedWriterRemainsOpen(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	done := make(chan error, 1)
	go func() { _, err := io.Copy(io.Discard, reader); done <- err }()
	if err := finishProbeRead(context.Background(), reader, done); !errors.Is(err, exec.ErrWaitDelay) {
		t.Fatalf("expected drain timeout, got %v", err)
	}
}

// A reader failure after process exit must reach the caller instead of turning
// a partial probe response into success.
func TestProbe_should_preserveFailure_whenReaderCannotDrain(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	_ = reader.Close()
	done := make(chan error, 1)
	go func() { _, err := io.Copy(io.Discard, reader); done <- err }()
	if err := finishProbeRead(context.Background(), reader, done); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("expected closed reader error, got %v", err)
	}
}
