package storage

import "database/sql"

// Earlier schemas have no provable recipient incarnation. Preserve consumption
// and uncertain attempt history, but invalidate consent that cannot be bound.
func migrateSessionTargets(tx *sql.Tx) error {
	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS session_targets (
device_uid TEXT PRIMARY KEY NOT NULL, epoch TEXT NOT NULL CHECK(epoch <> ''),
active INTEGER NOT NULL CHECK(active IN (0,1)));`); err != nil {
		return err
	}
	for _, table := range []string{"code_consumptions", "session_authorizations"} {
		for _, column := range []string{"target_uid", "target_epoch"} {
			var exists int
			if err := tx.QueryRow(`SELECT count(*) FROM pragma_table_info(?) WHERE name=?`, table, column).Scan(&exists); err != nil {
				return err
			}
			if exists == 0 {
				if _, err := tx.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + column + ` TEXT NOT NULL DEFAULT ''`); err != nil {
					return err
				}
			}
		}
	}
	_, err := tx.Exec(`
UPDATE resume_capabilities SET state='revoked' WHERE state='active';
UPDATE session_authorizations SET state='revoked' WHERE state='active';
UPDATE code_consumptions SET state='unknown' WHERE state='reserved';
UPDATE session_targets SET active=0;
CREATE INDEX IF NOT EXISTS session_target_idx ON session_authorizations(target_uid,target_epoch);
CREATE INDEX IF NOT EXISTS reservation_target_idx ON code_consumptions(target_uid,target_epoch);
PRAGMA user_version=12;`)
	return err
}
