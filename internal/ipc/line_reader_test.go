package ipc

import (
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Both directions must reject an oversized record without waiting for a
// newline or EOF from its sender, then close the affected connection.
func TestConn_should_closeOnOversizedUnterminatedRecord(t *testing.T) {
	for _, frame := range []bool{false, true} {
		left, right := net.Pipe()
		conn := NewConn(left)
		t.Cleanup(func() { left.Close(); right.Close() })
		go func() { _, _ = io.WriteString(right, strings.Repeat("x", protocol.MaxIPCMessageBytes+1)) }()
		done := make(chan error, 1)
		go func() {
			if frame {
				_, err := conn.ReadFrame()
				done <- err
			} else {
				_, err := conn.ReadRequest()
				done <- err
			}
		}()
		select {
		case err := <-done:
			if !errors.Is(err, ErrMessageTooLarge) || !conn.Closed() {
				t.Fatalf("error=%v closed=%v", err, conn.Closed())
			}
		case <-time.After(5 * time.Second):
			t.Fatal("oversized sender can hold the reader until newline")
		}
	}
}

// Maximum-sized JSON and the following coalesced record must both survive;
// buffered lookahead cannot be discarded between calls.
func TestConn_should_acceptLimitAndPreserveNextRecord(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	first := `{"id":"one","method":"status"}`
	wire := first + strings.Repeat(" ", protocol.MaxIPCMessageBytes-len(first)) + "\n" + `{"id":"two","method":"status"}` + "\n"
	go func() { _, _ = io.WriteString(right, wire) }()
	conn := NewConn(left)
	for _, expected := range []string{"one", "two"} {
		request, err := conn.ReadRequest()
		if err != nil || request.ID != expected {
			t.Fatalf("request=%+v error=%v", request, err)
		}
	}
}

// EOF cannot make an unfinished JSON record a valid terminal frame.
func TestLineReader_should_rejectPartialRecordAtEOF(t *testing.T) {
	_, err := newLineReader(strings.NewReader(`{"ok":true}`)).read()
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatal(err)
	}
}
