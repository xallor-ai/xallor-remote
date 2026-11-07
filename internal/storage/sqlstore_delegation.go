package storage

import (
	"crypto/rand"
	"database/sql"
	"fmt"
)

func migrateDelegation(tx *sql.Tx) error {
	var key [32]byte
	if _, err := rand.Read(key[:]); err != nil {
		return err
	}
	if _, err := tx.Exec(`CREATE TABLE execution_signing_key (singleton INTEGER PRIMARY KEY CHECK(singleton=1), key BLOB NOT NULL CHECK(length(key)=32));`); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO execution_signing_key VALUES (1,?)`, key[:]); err != nil {
		return err
	}
	_, err := tx.Exec(`PRAGMA user_version=6;`)
	return err
}

// ExecutionKey is loaded only by the Relay composition root. It survives
// restart so reconnecting workers can revalidate their original delegation.
func (s *SQLStore) ExecutionKey() ([32]byte, error) {
	var key [32]byte
	var raw []byte
	if err := s.db.QueryRow(`SELECT key FROM execution_signing_key WHERE singleton=1`).Scan(&raw); err != nil {
		return key, err
	}
	if len(raw) != len(key) {
		return key, fmt.Errorf("invalid execution signing key")
	}
	copy(key[:], raw)
	return key, nil
}
