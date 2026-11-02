package identity

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/principal"
)

// Credentials persist only in the profile, cannot move to another Relay, and a
// failed replacement must preserve the previously installed identity in memory.
func TestAccount_should_pinAndPreserveCredential_whenProfileChanges(t *testing.T) {
	s, err := LoadFrom(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	token, err := principal.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetAccount(token); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadFrom(s.Dir)
	if err != nil {
		t.Fatal(err)
	}
	_, got, err := loaded.Credentials()
	if err != nil || got != token {
		t.Fatal("credential did not survive restart", err)
	}
	if err := loaded.SetConfig("wss://different.invalid/remote", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loaded.Credentials(); err == nil {
		t.Fatal("credential moved to a new authority endpoint")
	}
	if err := loaded.SetAccount(""); err != nil {
		t.Fatal(err)
	}
	if _, got, err := loaded.Credentials(); err != nil || got != "" {
		t.Fatal("explicit clear failed", err)
	}
	// Replace the owned credential path with a directory to force atomic rename failure.
	if err := os.Remove(filepath.Join(s.Dir, "account.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(s.Dir, "account.json"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAccount(""); err == nil {
		t.Fatal("expected persistence failure")
	}
	if _, got, err := s.Credentials(); err != nil || got != token {
		t.Fatal("failed write changed active identity", err)
	}
}

// A damaged optional credential must block remote access without preventing the
// Runtime from starting its local import/clear recovery endpoint.
func TestAccount_should_allowExplicitRecovery_whenCredentialFileCorrupt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "account.json"), []byte(`{"token":`), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := LoadFrom(dir)
	if err != nil {
		t.Fatal("local recovery unavailable", err)
	}
	if relay, token, err := s.Credentials(); err == nil || relay != "" || token != "" {
		t.Fatal("bad credentials silently fell back", err)
	}
	if err := s.SetAccount("not-valid"); err == nil {
		t.Fatal("invalid replacement accepted")
	}
	if _, _, err := s.Credentials(); err == nil {
		t.Fatal("failed replacement cleared credential error")
	}
	if err := s.SetAccount(""); err != nil {
		t.Fatal(err)
	}
	if relay, token, err := s.Credentials(); err != nil || relay == "" || token != "" {
		t.Fatal("explicit recovery failed", err)
	}
	if s.AccountConfigured() {
		t.Fatal("clear retained configured state")
	}
}
