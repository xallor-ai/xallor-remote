package relay

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

type SQLStore struct {
	db *sql.DB
}

func OpenSQLStore(dir string) (*SQLStore, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	filename, err := filepath.Abs(filepath.Join(dir, "relay.db"))
	if err != nil {
		return nil, err
	}
	// Apply the short busy wait to every pooled connection, including replacements
	// after cancellation. A long native busy handler delays context cancellation.
	uriPath := filepath.ToSlash(filename)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	dsn := (&url.URL{Scheme: "file", Path: uriPath, RawQuery: "_pragma=busy_timeout%3D50"}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &SQLStore{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *SQLStore) migrate() error {
	if _, err := s.db.Exec(`PRAGMA journal_mode=WAL;`); err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var version int
	if err := tx.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	if version > 2 {
		return fmt.Errorf("unsupported Relay database version %d", version)
	}
	if version == 2 {
		return tx.Commit()
	}
	_, err = tx.Exec(`
CREATE TABLE IF NOT EXISTS devices (
  id TEXT PRIMARY KEY,
  secret_hash TEXT NOT NULL,
  grant_hash TEXT NOT NULL DEFAULT '',
  inbound INTEGER NOT NULL DEFAULT 0,
  last_seen INTEGER NOT NULL DEFAULT 0
);
DROP TABLE IF EXISTS grants;
DROP TABLE IF EXISTS audit;
PRAGMA user_version=2;
`)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLStore) Close() error {
	return s.db.Close()
}
