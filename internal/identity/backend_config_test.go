package identity

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/process"
)

// Unsupported selection must leave active/disk configuration unchanged and
// reject the complete patch before creating its requested workspace.
func TestBackendConfig_shouldRejectUnsupportedPatchWithoutSideEffects(t *testing.T) {
	s, err := LoadFrom(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetRuntimeConfig(Config{ExecutionBackend: process.NativeUser}); err != nil {
		t.Fatal(err)
	}
	before := s.ConfigSnapshot()
	disk, err := os.ReadFile(filepath.Join(s.Dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"low_privilege", "cgroup", "container", "typo"} {
		workspace := filepath.Join(t.TempDir(), "must-not-create")
		err := s.SetRuntimeConfig(Config{ExecutionBackend: name, Workspace: workspace, RelayURL: "ws://changed.invalid", Shell: "auto"})
		if !errors.Is(err, process.ErrBackendUnavailable) || s.ConfigSnapshot() != before {
			t.Fatalf("unsupported patch published: %s %v", name, err)
		}
		after, err := os.ReadFile(filepath.Join(s.Dir, "config.json"))
		if err != nil || !bytes.Equal(after, disk) {
			t.Fatal("unsupported patch changed disk", err)
		}
		if _, err := os.Stat(workspace); !os.IsNotExist(err) {
			t.Fatal("unsupported patch created workspace", err)
		}
	}
	again, err := LoadFrom(s.Dir)
	if err != nil || again.ConfigSnapshot().ExecutionBackend != process.NativeUser {
		t.Fatal("native selection not persisted", err)
	}
}

// Persisted unsupported configuration must block Runtime startup before a
// device identity or workspace is created, never silently selecting native.
func TestBackendConfig_shouldRejectUnsupportedPersistedSelection(t *testing.T) {
	dir := t.TempDir()
	workspace := filepath.Join(dir, "workspace")
	raw, _ := json.Marshal(Config{ExecutionBackend: "container", Workspace: workspace})
	if err := os.WriteFile(filepath.Join(dir, "config.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFrom(dir); !errors.Is(err, process.ErrBackendUnavailable) {
		t.Fatalf("unsupported persisted config loaded: %v", err)
	}
	for _, name := range []string{"workspace", "identity.db"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("startup created %s: %v", name, err)
		}
	}
}
