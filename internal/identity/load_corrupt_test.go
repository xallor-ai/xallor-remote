package identity

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Corrupt configuration must fail before identity creation or hosted Relay
// fallback. Recovery is explicit: remove/restore the bad file first.
func TestLoadFrom_should_failClosedOnCorruptConfig(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFrom(dir); err == nil || !strings.Contains(err.Error(), "invalid config.json") {
		t.Fatalf("expected config parse error, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "identity.db")); !os.IsNotExist(err) {
		t.Fatalf("corrupt config created identity: %v", err)
	}
}

func TestLoadFrom_should_failClosedOnCorruptPeers(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "peers.json"), []byte("[]oops"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFrom(dir); err == nil || !strings.Contains(err.Error(), "invalid peers.json") {
		t.Fatalf("expected peers parse error, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "identity.db")); !os.IsNotExist(err) {
		t.Fatalf("corrupt peers created identity: %v", err)
	}
}
