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
	got, trunc := sliceRead(raw, protocol.ReadPayload{Tail: 1}, 64*1024)
	if trunc || !bytes.Equal(got, []byte("b")) {
		t.Fatalf("tail:1 on trailing newline: %q trunc=%v", got, trunc)
	}
	got, trunc = sliceRead(raw, protocol.ReadPayload{Head: 1}, 64*1024)
	if trunc || !bytes.Equal(got, []byte("a")) {
		t.Fatalf("head:1: %q trunc=%v", got, trunc)
	}
}

func TestSliceReadHeadTail(t *testing.T) {
	raw := []byte("a\nb\nc\nd")
	got, trunc := sliceRead(raw, protocol.ReadPayload{Head: 2}, 64*1024)
	if trunc || !bytes.Equal(got, []byte("a\nb")) {
		t.Fatalf("head: %q trunc=%v", got, trunc)
	}
	got, trunc = sliceRead(raw, protocol.ReadPayload{Tail: 2}, 64*1024)
	if trunc || !bytes.Equal(got, []byte("c\nd")) {
		t.Fatalf("tail: %q trunc=%v", got, trunc)
	}
}

func TestSliceRead_should_use_bytes_when_offset_and_limit_set(t *testing.T) {
	raw := []byte("abcdef")
	got, trunc := sliceRead(raw, protocol.ReadPayload{Offset: 2, Limit: 3}, 64*1024)
	if trunc || !bytes.Equal(got, []byte("cde")) {
		t.Fatalf("offset/limit: %q trunc=%v", got, trunc)
	}
}

// Default window is 64 KiB. A larger file must be marked truncated and stay valid UTF-8.
func TestSliceRead_should_markTruncatedOnRuneBoundary_whenDefaultWindowCutsCJK(t *testing.T) {
	raw := bytes.Repeat([]byte("中"), protocol.DefaultReadBytes)
	got, trunc := sliceRead(raw, protocol.ReadPayload{}, protocol.DefaultReadBytes)
	if !trunc {
		t.Fatal("default window on oversized file should be truncated")
	}
	if !utf8.Valid(got) {
		t.Fatalf("cut mid-rune: %q", got)
	}
	if bytes.ContainsRune(got, '\uFFFD') {
		t.Fatal("replacement rune leaked into read window")
	}
	if len(got) == 0 || len(got) > protocol.DefaultReadBytes {
		t.Fatalf("len=%d", len(got))
	}
}

func TestSliceRead_should_notMarkTruncated_whenFileFitsDefaultWindow(t *testing.T) {
	raw := []byte("short")
	got, trunc := sliceRead(raw, protocol.ReadPayload{}, protocol.DefaultReadBytes)
	if trunc || !bytes.Equal(got, raw) {
		t.Fatalf("got=%q trunc=%v", got, trunc)
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

// Incomplete trailing rune after a byte cut must be dropped, not emitted as U+FFFD.
func TestNextUTF8Frame_should_dropInvalidRemainder_whenInputEndsMidRune(t *testing.T) {
	raw := append([]byte("中"), []byte{0xe4}...)
	frame, rest := nextUTF8Frame(raw, len(raw))
	if string(frame) != "中" || !utf8.Valid(frame) {
		t.Fatalf("frame=%q", frame)
	}
	if len(rest) == 0 {
		t.Fatal("expected leftover continuation")
	}
	frame, rest = nextUTF8Frame(rest, 64)
	if len(frame) != 0 || len(rest) != 0 {
		t.Fatalf("invalid remainder should be dropped frame=%q rest=%q", frame, rest)
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

func TestReadTooLarge_should_reject_before_read_when_default_whole_file(t *testing.T) {
	if !readTooLarge(int64(protocol.MaxReadBytes)+1, protocol.ReadPayload{}) {
		t.Fatal("default oversize should reject")
	}
	if readTooLarge(int64(protocol.MaxReadBytes)+1, protocol.ReadPayload{Head: 1}) {
		t.Fatal("head may read then slice")
	}
}

func TestPathDenied_should_match_os_root_escape(t *testing.T) {
	if !pathDenied(errStr("openat junc\\secret.txt: path escapes from parent")) {
		t.Fatal("os.Root escape must be policy_deny")
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
