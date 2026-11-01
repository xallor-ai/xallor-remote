package noderun

import (
	"bytes"
	"testing"
	"unicode/utf8"

	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

func TestEvalRepro_should_survive_whenOffsetEqualsFileLen(t *testing.T) {
	raw := []byte("hello")
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("P1 panic offset==len(raw): %v", r)
		}
	}()
	got, trunc := sliceRead(raw, protocol.ReadPayload{Offset: len(raw)}, 64*1024)
	if len(got) != 0 || trunc {
		t.Fatalf("EOF offset should be empty got=%q trunc=%v", got, trunc)
	}
	if off := snapUTF8Start(raw, len(raw)); off != len(raw) {
		t.Fatalf("snapUTF8Start(len)=%d", off)
	}
}

func TestEvalRepro_should_markTruncated_whenExplicitLimitCutsCJK(t *testing.T) {
	raw := bytes.Repeat([]byte("中"), protocol.DefaultReadBytes)
	got, trunc := sliceRead(raw, protocol.ReadPayload{Limit: protocol.DefaultReadBytes}, protocol.DefaultReadBytes)
	frame, rest := nextUTF8Frame(got, len(got))
	t.Logf("explicit limit=65536: sliceLen=%d trunc=%v valid=%v frame=%d rest=%d",
		len(got), trunc, utf8.Valid(got), len(frame), len(rest))
	if !trunc {
		t.Error("P2: explicit 65536 window cut CJK but truncated=false")
	}
	if !utf8.Valid(got) {
		t.Errorf("P2: explicit window not valid UTF-8 len=%d", len(got))
	}
}

func TestEvalRepro_should_notReturnEmpty_whenLimitCutsFirstCJK(t *testing.T) {
	raw := []byte("中X")
	got, trunc := sliceRead(raw, protocol.ReadPayload{Limit: 1}, 1)
	frame, rest := nextUTF8Frame(got, 64)
	t.Logf("limit=1 on 中X: slice=%q sliceLen=%d trunc=%v valid=%v frame=%q rest=%q",
		got, len(got), trunc, utf8.Valid(got), frame, rest)
	if len(frame) == 0 && len(got) != 0 {
		t.Error("P2: send stage would drop mid-rune window to empty")
	}
	if !trunc {
		t.Error("P2: discarded tail not marked truncated")
	}
}
