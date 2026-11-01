package noderun

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

func TestResolveInWorkspace(t *testing.T) {
	ws := t.TempDir()
	inside, err := resolveInWorkspace(ws, "a/b.txt")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(ws, "a", "b.txt")
	if inside != want {
		t.Fatalf("got %s want %s", inside, want)
	}
	if _, err := resolveInWorkspace(ws, filepath.Join(ws, "..", "outside.txt")); err == nil {
		t.Fatal("expected deny for parent traversal")
	}
	absOut := filepath.Join(os.TempDir(), "xallor-outside.txt")
	if _, err := resolveInWorkspace(ws, absOut); err == nil {
		t.Fatal("expected deny for abs outside")
	}
}

func TestReadWriteWorkspace_should_roundtrip_inside_root(t *testing.T) {
	ws := t.TempDir()
	if err := writeWorkspaceFile(ws, filepath.Join("dir", "a.txt"), "hello"); err != nil {
		t.Fatal(err)
	}
	got, err := readWorkspaceFile(ws, filepath.Join("dir", "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Fatalf("got %q", got)
	}
}

func TestWorkspaceRel_should_deny_when_path_exceeds_limit(t *testing.T) {
	ws := t.TempDir()
	p := strings.Repeat("a", protocol.MaxPathBytes+1)
	if _, err := workspaceRel(ws, p); err == nil {
		t.Fatal("expected deny for long path")
	}
}
