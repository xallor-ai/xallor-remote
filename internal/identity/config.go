package identity

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/xallor-ai/xallor-remote/internal/atomicfile"
	"github.com/xallor-ai/xallor-remote/internal/process"
	"github.com/xallor-ai/xallor-remote/internal/shell"
)

// A failed setting must leave both active and persisted configuration intact.
// Shell validity is checked before creating a requested workspace directory.
func (s *Store) SetConfig(relayURL, workspace, name string) error {
	return s.SetRuntimeConfig(Config{RelayURL: relayURL, Workspace: workspace, Shell: name})
}

// Validate the whole patch before filesystem writes. An unsupported backend
// must not partially change the shell, workspace or persisted configuration.
func (s *Store) SetRuntimeConfig(update Config) error {
	if _, err := process.Select(update.ExecutionBackend); err != nil {
		return err
	}
	if err := shell.Validate(update.Shell); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.Config
	if update.RelayURL != "" {
		next.RelayURL = update.RelayURL
	}
	if update.Workspace != "" {
		if err := os.MkdirAll(update.Workspace, 0700); err != nil {
			return err
		}
		next.Workspace = update.Workspace
	}
	if update.Shell != "" {
		next.Shell = update.Shell
	}
	if update.ExecutionBackend != "" {
		next.ExecutionBackend = update.ExecutionBackend
	}
	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicfile.Write(filepath.Join(s.Dir, "config.json"), data); err != nil {
		return err
	}
	s.Config = next
	return nil
}

// ConfigSnapshot returns a consistent copy; callers must not read Config while
// another IPC head may be changing the target's shell or workspace.
func (s *Store) ConfigSnapshot() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Config
}
