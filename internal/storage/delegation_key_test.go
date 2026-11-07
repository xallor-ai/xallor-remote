package storage

import "testing"

// Reopening preserves the signing key; upgrading schema 5 creates a fresh key
// without replacing the authority or device identities.
func TestDelegationKey_should_surviveReopenAndPreserveAuthority_whenUpgraded(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenSQLStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	key, err := s.ExecutionKey()
	if err != nil || key == [32]byte{} {
		t.Fatal("missing signing key", err)
	}
	authority := s.Authority()
	s.Close()
	s, err = OpenSQLStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.ExecutionKey()
	if err != nil || again != key || s.Authority() != authority {
		t.Fatal("restart changed authority", err)
	}
	if _, err = s.db.Exec(`DROP TABLE execution_signing_key; PRAGMA user_version=5;`); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = OpenSQLStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	upgraded, err := s.ExecutionKey()
	if err != nil || upgraded == key || upgraded == [32]byte{} || s.Authority() != authority {
		t.Fatal("invalid upgrade", err)
	}
}
