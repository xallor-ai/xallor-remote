package noderun

import (
	"bytes"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

func TestSliceReadHeadTail(t *testing.T) {
	raw := []byte("a\nb\nc\nd")
	got := sliceRead(raw, protocol.ReadPayload{Head: 2}, 64*1024)
	if !bytes.Equal(got, []byte("a\nb")) {
		t.Fatalf("head: %q", got)
	}
	got = sliceRead(raw, protocol.ReadPayload{Tail: 2}, 64*1024)
	if !bytes.Equal(got, []byte("c\nd")) {
		t.Fatalf("tail: %q", got)
	}
}
