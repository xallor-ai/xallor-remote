package relay

import "fmt"

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
	if version > 5 {
		return fmt.Errorf("unsupported Relay database version %d", version)
	}
	if version == 5 {
		return tx.Commit()
	}
	if version < 3 {
		// Normalize earlier development schemas in one transaction. Runtime queries
		// use only the final shape; the display ID, credentials and inbound state survive.
		_, err = tx.Exec(`
CREATE TABLE IF NOT EXISTS devices (
  id TEXT PRIMARY KEY, secret_hash TEXT NOT NULL, grant_hash TEXT NOT NULL DEFAULT '',
  inbound INTEGER NOT NULL DEFAULT 0, last_seen INTEGER NOT NULL DEFAULT 0
);
DROP TABLE IF EXISTS grants;
DROP TABLE IF EXISTS audit;
CREATE TABLE devices_next (
  uid TEXT PRIMARY KEY NOT NULL,
  id TEXT NOT NULL UNIQUE,
  secret_hash TEXT NOT NULL,
  inbound INTEGER NOT NULL DEFAULT 0,
  last_seen INTEGER NOT NULL DEFAULT 0
);
INSERT INTO devices_next (uid,id,secret_hash,inbound,last_seen)
SELECT lower(hex(randomblob(16))),id,secret_hash,inbound,last_seen FROM devices;
CREATE TABLE device_grants (
  id TEXT PRIMARY KEY NOT NULL,
  device_uid TEXT NOT NULL UNIQUE REFERENCES devices_next(uid) ON DELETE CASCADE,
  token_hash TEXT NOT NULL CHECK(token_hash <> '')
);
INSERT INTO device_grants (id,device_uid,token_hash)
SELECT lower(hex(randomblob(16))),n.uid,d.grant_hash FROM devices d
JOIN devices_next n ON d.id=n.id WHERE d.grant_hash <> '';
DROP TABLE devices;
ALTER TABLE devices_next RENAME TO devices;
PRAGMA user_version=3;
`)
		if err != nil {
			return err
		}
	}
	if version < 4 {
		if err := migratePrincipals(tx); err != nil {
			return err
		}
	}
	if err := migrateAccess(tx); err != nil {
		return err
	}
	return tx.Commit()
}
