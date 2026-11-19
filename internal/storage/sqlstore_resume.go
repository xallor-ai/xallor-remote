package storage

import (
	"database/sql"
	"fmt"
)

func migrateResumeCapabilities(tx *sql.Tx) error {
	var kind string
	err := tx.QueryRow(`SELECT type FROM sqlite_master WHERE name='resume_capabilities'`).Scan(&kind)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if err == nil && kind != "table" {
		return fmt.Errorf("resume-capability object is %s, expected table", kind)
	}
	_, err = tx.Exec(`
CREATE TABLE IF NOT EXISTS resume_capabilities (
  resume_id TEXT PRIMARY KEY NOT NULL,
  session_id TEXT NOT NULL REFERENCES session_authorizations(session_id) ON DELETE CASCADE,
  binding_kind TEXT NOT NULL,
  binding_digest TEXT NOT NULL,
  capability_sha256 TEXT NOT NULL CHECK(length(capability_sha256)=64),
  issued_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  generation INTEGER NOT NULL CHECK(generation > 0),
  state TEXT NOT NULL CHECK(state IN ('active','consumed','revoked','unknown'))
);
CREATE INDEX IF NOT EXISTS resume_capabilities_session_idx ON resume_capabilities(session_id);
CREATE INDEX IF NOT EXISTS resume_capabilities_binding_idx ON resume_capabilities(binding_kind,binding_digest,state);
PRAGMA user_version=10;`)
	return err
}
