package noderun

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

func TestSliceRead_should_keep_last_line_when_file_ends_with_newline(t *testing.T) {
	raw := []byte("a\nb\n")
	got := sliceRead(raw, protocol.ReadPayload{Tail: 1}, 64*1024)
	if !bytes.Equal(got, []byte("b")) {
		t.Fatalf("tail:1 on trailing newline: %q", got)
	}
	got = sliceRead(raw, protocol.ReadPayload{Head: 1}, 64*1024)
	if !bytes.Equal(got, []byte("a")) {
		t.Fatalf("head:1: %q", got)
	}
}

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

func TestSliceRead_should_use_bytes_when_offset_and_limit_set(t *testing.T) {
	raw := []byte("abcdef")
	got := sliceRead(raw, protocol.ReadPayload{Offset: 2, Limit: 3}, 64*1024)
	if !bytes.Equal(got, []byte("cde")) {
		t.Fatalf("offset/limit: %q", got)
	}
}

func TestNextUTF8Frame_should_split_on_rune_boundary(t *testing.T) {
	raw := []byte("中文x")
	frame, rest := nextUTF8Frame(raw, 4)
	if !utf8.Valid(frame) || string(frame) != "中" {
		t.Fatalf("frame=%q rest=%q", frame, rest)
	}
	if string(rest) != "文x" {
		t.Fatalf("rest=%q", rest)
	}
}

func TestNextUTF8Frame_should_respect_budget_for_ascii(t *testing.T) {
	raw := bytes.Repeat([]byte("x"), protocol.MaxFrameBytes+8)
	frame, rest := nextUTF8Frame(raw, stdoutFrameBudget())
	if len(frame) != protocol.MaxFrameBytes {
		t.Fatalf("len=%d", len(frame))
	}
	if len(rest) != 8 {
		t.Fatalf("rest=%d", len(rest))
	}
}

func TestWrapCommand_should_only_set_encoding(t *testing.T) {
	got := wrapCommand("Write-Error \"x\"")
	if strings.Contains(got, "LASTEXITCODE") {
		t.Fatalf("wrapper must not promote LASTEXITCODE: %s", got)
	}
	if !strings.HasSuffix(got, "Write-Error \"x\"") {
		t.Fatalf("command not preserved: %s", got)
	}
}
