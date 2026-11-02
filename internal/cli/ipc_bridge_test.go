package cli

import (
	"io"
	"net"
	"testing"
	"time"
)

// A head uses a duplex session and EOF closes the Runtime side without a replay.
func TestBridge_should_forwardAndClose_whenHeadEnds(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	in, input := io.Pipe()
	defer in.Close()
	defer input.Close()
	output, out := io.Pipe()
	defer output.Close()
	defer out.Close()
	done := make(chan error, 1)
	go func() { done <- bridgeIPC(a, in, out) }()
	go func() { _, _ = input.Write([]byte("request\n")) }()
	buf := make([]byte, 8)
	_ = b.SetDeadline(time.Now().Add(time.Second))
	if _, err := io.ReadFull(b, buf); err != nil || string(buf) != "request\n" {
		t.Fatalf("request %q %v", buf, err)
	}
	go func() { _, _ = b.Write([]byte("response\n")) }()
	buf = make([]byte, 9)
	if _, err := io.ReadFull(output, buf); err != nil || string(buf) != "response\n" {
		t.Fatalf("response %q %v", buf, err)
	}
	_ = input.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("bridge did not close")
	}
	if _, err := b.Read(buf); err == nil {
		t.Fatal("Runtime connection stayed open")
	}
}
