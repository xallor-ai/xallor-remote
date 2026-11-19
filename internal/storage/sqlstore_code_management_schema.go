package storage

import (
	"database/sql"
	"fmt"
)

func migrateCodeManagement(tx *sql.Tx) error {
	var kind string
	err := tx.QueryRow(`SELECT type FROM sqlite_master WHERE name='code_rotations'`).Scan(&kind)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if err == nil && kind != "table" {
		return fmt.Errorf("code rotation object is %s, expected table", kind)
	}
	_, err = tx.Exec(`CREATE TABLE IF NOT EXISTS code_rotations (
old_code_id TEXT PRIMARY KEY NOT NULL REFERENCES access_codes(code_id),
new_code_id TEXT UNIQUE NOT NULL REFERENCES access_codes(code_id),
intent_sha256 TEXT NOT NULL CHECK(length(intent_sha256)=64), CHECK(old_code_id<>new_code_id)
);
UPDATE resume_capabilities SET state='revoked' WHERE state='active' AND session_id IN (
 SELECT s.session_id FROM session_authorizations s JOIN access_codes c ON c.code_id=s.code_id AND c.generation=s.generation WHERE c.state='revoked');
UPDATE session_authorizations SET state='revoked' WHERE state IN ('active','pending') AND EXISTS (
 SELECT 1 FROM access_codes c WHERE c.code_id=session_authorizations.code_id AND c.generation=session_authorizations.generation AND c.state='revoked');
UPDATE code_consumptions SET state='unknown' WHERE state='reserved' AND EXISTS (
 SELECT 1 FROM access_codes c WHERE c.code_id=code_consumptions.code_id AND c.generation=code_consumptions.generation AND c.state='revoked');
PRAGMA user_version=13;`)
	return err
}
