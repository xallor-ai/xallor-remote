package noderun

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"unicode/utf8"

	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

func TestReadWorkspaceWindow_should_boundSelectedWindows(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "large.txt")
	if err := os.WriteFile(path, bytes.Repeat([]byte("line\n"), protocol.MaxReadBytes/5+100), 0600); err != nil {
		t.Fatal(err)
	}
	got, _, err := readWorkspaceWindow(workspace, protocol.ReadPayload{Head: 1, Path: "large.txt"}, int64(protocol.MaxReadBytes*2))
	if err != nil || string(got) != "line\n" {
		t.Fatalf("head window len=%d err=%v", len(got), err)
	}
	got, _, err = readWorkspaceWindow(workspace, protocol.ReadPayload{Tail: 1, Path: "large.txt"}, int64(protocol.MaxReadBytes*2))
	if err != nil || string(got) != "line\n" {
		t.Fatalf("tail window len=%d err=%v", len(got), err)
	}
	got, _, err = readWorkspaceWindow(workspace, protocol.ReadPayload{Offset: protocol.MaxReadBytes + 1, Limit: 8, Path: "large.txt"}, int64(protocol.MaxReadBytes*2))
	if err != nil || len(got) != 8 {
		t.Fatalf("offset window len=%d err=%v", len(got), err)
	}
}

func TestReadWorkspaceWindow_should_boundHugeTailLineAndMarkTruncated(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "one-line.txt")
	if err := os.WriteFile(path, bytes.Repeat([]byte("中"), protocol.MaxReadBytes*3), 0600); err != nil {
		t.Fatal(err)
	}
	got, truncated, err := readWorkspaceWindow(workspace, protocol.ReadPayload{Tail: 1, Path: "one-line.txt"}, int64(protocol.MaxReadBytes*3))
	if err != nil || !truncated || len(got) > protocol.MaxReadBytes+4 || !utf8.Valid(got) {
		t.Fatalf("tail len=%d truncated=%v valid=%v err=%v", len(got), truncated, utf8.Valid(got), err)
	}
}

func TestReadWorkspaceWindow_should_preserveUTF8OffsetAndLineSelection(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "utf.txt"), []byte("中x"), 0600); err != nil {
		t.Fatal(err)
	}
	got, _, err := readWorkspaceWindow(workspace, protocol.ReadPayload{Offset: 1, Limit: 4, Path: "utf.txt"}, 4)
	if err != nil || string(got) != "中x" {
		t.Fatalf("offset=%q err=%v", got, err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "lines.txt"), []byte("a\nb\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got, _, err = readWorkspaceWindow(workspace, protocol.ReadPayload{Head: 1, Path: "lines.txt"}, 64)
	if err != nil || string(got) != "a\n" {
		t.Fatalf("head window=%q err=%v", got, err)
	}
	_, trunc := sliceRead(got, protocol.ReadPayload{Head: 1}, 64)
	if trunc {
		t.Fatal("head should not truncate")
	}
	got, _, err = readWorkspaceWindow(workspace, protocol.ReadPayload{Tail: 1, Limit: 2, Path: "lines.txt"}, 64)
	if err != nil || string(got) != "b\n" {
		t.Fatalf("tail selected window=%q err=%v", got, err)
	}
}

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

// Follow-up at DefaultReadBytes (not the snapped length) must still return the rest, not look like EOF.
func TestSliceRead_should_keepFollowingCJK_whenOffsetLandsMidRune(t *testing.T) {
	raw := bytes.Repeat([]byte("中"), protocol.DefaultReadBytes)
	first, trunc := sliceRead(raw, protocol.ReadPayload{}, protocol.DefaultReadBytes)
	if !trunc || !utf8.Valid(first) || len(first) == 0 {
		t.Fatalf("first window len=%d trunc=%v", len(first), trunc)
	}
	got, _ := sliceRead(raw, protocol.ReadPayload{Offset: protocol.DefaultReadBytes}, protocol.DefaultReadBytes)
	if len(got) == 0 {
		t.Fatal("mid-rune offset dropped the rest of the file")
	}
	if !utf8.Valid(got) || bytes.ContainsRune(got, '\uFFFD') {
		t.Fatalf("follow-up window invalid: %q", got[:min(12, len(got))])
	}
	if !bytes.HasPrefix(got, []byte("中")) {
		t.Fatalf("expected snapped rune, got %q", got[:min(12, len(got))])
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

func TestNextUTF8Frame_should_skipLeadingContinuation_whenMoreRunesFollow(t *testing.T) {
	raw := append([]byte{0xb8, 0xad}, []byte("中")...)
	frame, rest := nextUTF8Frame(raw, 16)
	if string(frame) != "中" || len(rest) != 0 {
		t.Fatalf("frame=%q rest=%q", frame, rest)
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
