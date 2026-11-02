package relay

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type SQLStore struct {
	db *sql.DB
}

type AuditEntry struct {
	DeviceID    string
	Op          string
	ExecID      string
	Decision    string
	Code        string
	DurationMS  int64
	ArgsPreview string
	ArgsDigest  string
}

type AuditStore interface {
	AppendAudit(AuditEntry)
}

func OpenSQLStore(dir string) (*SQLStore, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "relay.db"))
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
	if _, err := s.db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;`); err != nil {
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
	if version > 1 {
		return fmt.Errorf("unsupported Relay database version %d", version)
	}
	if version == 1 {
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
CREATE TABLE IF NOT EXISTS audit (
  ts INTEGER NOT NULL,
  device_id TEXT,
  op TEXT,
  exec_id TEXT,
  decision TEXT,
  code TEXT,
  duration_ms INTEGER,
  args_preview TEXT,
  args_digest TEXT
);
CREATE INDEX IF NOT EXISTS audit_ts ON audit(ts);
PRAGMA user_version=1;
`)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLStore) Close() error {
	return s.db.Close()
}

func (s *SQLStore) AppendAudit(e AuditEntry) {
	_, _ = s.db.Exec(
		`INSERT INTO audit (ts, device_id, op, exec_id, decision, code, duration_ms, args_preview, args_digest)
VALUES (?,?,?,?,?,?,?,?,?)`,
		time.Now().Unix(), e.DeviceID, e.Op, e.ExecID, e.Decision, e.Code, e.DurationMS, e.ArgsPreview, e.ArgsDigest,
	)
}
