package storage

import "database/sql"

// Earlier WIP versions stored the code's full scope instead of recipient
// consent. That consent cannot be reconstructed, so preserve consumption
// history while invalidating old active authorizations in the migration commit.
func migrateSessionConsent(tx *sql.Tx) error {
	_, err := tx.Exec(`
UPDATE session_authorizations SET state='revoked' WHERE state='active';
UPDATE resume_capabilities SET state='revoked' WHERE state='active';
PRAGMA user_version=11;`)
	return err
}
