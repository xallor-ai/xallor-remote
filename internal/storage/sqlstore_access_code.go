package storage

import (
	"database/sql"
	"fmt"
)

func migrateAccessCodes(tx *sql.Tx) error {
	for _, name := range []string{"access_codes", "code_consumptions", "session_authorizations"} {
		var kind string
		err := tx.QueryRow(`SELECT type FROM sqlite_master WHERE name=?`, name).Scan(&kind)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return err
		}
		if kind != "table" {
			return fmt.Errorf("access-code object %s is %s, expected table", name, kind)
		}
	}
	_, err := tx.Exec(`
CREATE TABLE IF NOT EXISTS access_codes (
  code_id TEXT PRIMARY KEY NOT NULL,
  device_uid TEXT NOT NULL,
  ownership_id TEXT NOT NULL DEFAULT '',
  type TEXT NOT NULL CHECK(type IN ('one_time','expiring','long_lived')),
  secret_hash TEXT NOT NULL CHECK(secret_hash <> ''),
  issued_at INTEGER NOT NULL,
  expires_at INTEGER,
  generation INTEGER NOT NULL CHECK(generation > 0),
  scope_json TEXT NOT NULL,
  require_approval INTEGER NOT NULL CHECK(require_approval IN (0,1)),
  state TEXT NOT NULL CHECK(state IN ('active','revoked','expired','consumed')),
  UNIQUE(device_uid, generation)
);
CREATE TABLE IF NOT EXISTS code_consumptions (
  reservation_id TEXT PRIMARY KEY NOT NULL,
  code_id TEXT NOT NULL REFERENCES access_codes(code_id) ON DELETE CASCADE,
  generation INTEGER NOT NULL CHECK(generation > 0),
  session_id TEXT NOT NULL,
  state TEXT NOT NULL CHECK(state IN ('reserved','consumed','released','unknown')),
  exclusive INTEGER NOT NULL DEFAULT 0 CHECK(exclusive IN (0,1)),
  reserved_at INTEGER NOT NULL,
  reservation_expires_at INTEGER NOT NULL,
  terminal_at INTEGER
);
CREATE TABLE IF NOT EXISTS session_authorizations (
  session_id TEXT PRIMARY KEY NOT NULL,
  code_id TEXT NOT NULL REFERENCES access_codes(code_id) ON DELETE CASCADE,
  generation INTEGER NOT NULL CHECK(generation > 0),
  scope_json TEXT NOT NULL,
  issued_at INTEGER NOT NULL,
  expires_at INTEGER,
  state TEXT NOT NULL CHECK(state IN ('pending','active','ended','revoked','unknown'))
);
CREATE INDEX IF NOT EXISTS code_consumptions_code_idx ON code_consumptions(code_id,generation);
CREATE INDEX IF NOT EXISTS session_authorizations_code_idx ON session_authorizations(code_id,generation);
CREATE UNIQUE INDEX IF NOT EXISTS code_consumptions_one_time_idx ON code_consumptions(code_id,generation)
  WHERE state='reserved' AND exclusive=1;
PRAGMA user_version=9;`)
	return err
}
