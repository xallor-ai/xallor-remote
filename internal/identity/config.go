package identity

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/xallor-ai/xallor-remote/internal/atomicfile"
	"github.com/xallor-ai/xallor-remote/internal/shell"
)

// A failed setting must leave both active and persisted configuration intact.
// Shell validity is checked before creating a requested workspace directory.
func (s *Store) SetConfig(relayURL, workspace, name string) error {
	if err := shell.Validate(name); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.Config
	if relayURL != "" {
		next.RelayURL = relayURL
	}
	if workspace != "" {
		if err := os.MkdirAll(workspace, 0700); err != nil {
			return err
		}
		next.Workspace = workspace
	}
	if name != "" {
		next.Shell = name
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
