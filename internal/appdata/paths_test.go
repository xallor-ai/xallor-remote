package appdata

import (
	"path/filepath"
	"strings"
	"testing"
)

// Explicit profiles must isolate both identity and IPC from the user's default instance.
func TestProfile_should_isolateDataWorkspaceAndIPC(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XALLOR_REMOTE_DATA_DIR", root)
	dir, err := DataDir()
	if err != nil || dir != root {
		t.Fatalf("%s %v", dir, err)
	}
	ws, err := DefaultWorkspace()
	if err != nil || ws != filepath.Join(root, "workspace") {
		t.Fatalf("%s %v", ws, err)
	}
	first, err := IPCPath()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("XALLOR_REMOTE_DATA_DIR", filepath.Join(root, "second"))
	second, _ := IPCPath()
	if first == second || strings.HasSuffix(first, "pipe\\XallorRemote") {
		t.Fatal("profile IPC not isolated")
	}
}

// Relative overrides must not silently select a different profile after cwd changes.
func TestProfile_should_rejectRelativeDirectory(t *testing.T) {
	t.Setenv("XALLOR_REMOTE_DATA_DIR", "relative")
	if _, err := DataDir(); err == nil {
		t.Fatal("relative data directory accepted")
	}
	if _, err := IPCPath(); err == nil {
		t.Fatal("relative IPC accepted")
	}
}
