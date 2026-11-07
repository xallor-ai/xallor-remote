package identity

import (
	"os"
	"path/filepath"
	"testing"
)

// Rejecting an interpreter must preserve the whole configuration and avoid
// creating the requested workspace as a side effect of an invalid request.
func TestConfig_should_preserveSettings_whenShellRejected(t *testing.T) {
	s, err := LoadFrom(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	before := s.ConfigSnapshot()
	path := filepath.Join(t.TempDir(), "absent")
	if s.SetConfig("ws://changed.invalid", path, "arbitrary --args") == nil {
		t.Fatal("invalid shell accepted")
	}
	if s.ConfigSnapshot() != before {
		t.Fatal("invalid request changed settings")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("invalid request created workspace", err)
	}
}

// Publication failure cannot change the active interpreter. A successful
// explicit auto selection remains visible after reopening the profile.
func TestConfig_should_publishOnlyAfterPersisting(t *testing.T) {
	s, err := LoadFrom(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetConfig("", "", "auto"); err != nil {
		t.Fatal(err)
	}
	again, err := LoadFrom(s.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if again.ConfigSnapshot().Shell != "auto" {
		t.Fatal("shell not persisted")
	}
	before := s.ConfigSnapshot()
	// The test owns this empty profile; a directory at the destination forces
	// atomic replacement to fail without relying on platform permission rules.
	if err := os.Remove(filepath.Join(s.Dir, "config.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(s.Dir, "config.json"), 0700); err != nil {
		t.Fatal(err)
	}
	if s.SetConfig("ws://changed.invalid", "", "pwsh") == nil {
		t.Fatal("failed publication accepted")
	}
	if s.ConfigSnapshot() != before {
		t.Fatal("failed publication changed active settings")
	}
}
