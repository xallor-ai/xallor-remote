package noderun

import (
	"bytes"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

func TestTakeChunkStopsAfterCap(t *testing.T) {
	sent := protocol.MaxExecOutputBytes - 10
	send, next, trunc, drain := takeChunk(sent, bytes.Repeat([]byte("a"), 32))
	if !trunc || !drain || len(send) != 10 || next != protocol.MaxExecOutputBytes {
		t.Fatalf("send=%d next=%d trunc=%v drain=%v", len(send), next, trunc, drain)
	}
	send, _, trunc, drain = takeChunk(next, []byte("more"))
	if send != nil || !trunc || !drain {
		t.Fatal("expected drain after cap")
	}
}

func TestTakeChunkClipsFrame(t *testing.T) {
	big := bytes.Repeat([]byte("b"), protocol.MaxFrameBytes+8)
	send, next, trunc, drain := takeChunk(0, big)
	if len(send) != protocol.MaxFrameBytes || !trunc || drain || next != protocol.MaxFrameBytes {
		t.Fatalf("len=%d trunc=%v drain=%v next=%d", len(send), trunc, drain, next)
	}
}
