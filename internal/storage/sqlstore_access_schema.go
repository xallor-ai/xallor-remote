package storage

import "database/sql"

func migrateAccess(tx *sql.Tx) error {
	_, err := tx.Exec(`
CREATE TABLE device_ownership (
 device_uid TEXT PRIMARY KEY NOT NULL REFERENCES devices(uid) ON DELETE CASCADE,
 id TEXT NOT NULL UNIQUE,
 tenant_id TEXT NOT NULL REFERENCES tenants(id),
 principal_id TEXT NOT NULL REFERENCES principals(id)
);
CREATE TABLE device_shares (
 id TEXT PRIMARY KEY NOT NULL,
 ownership_id TEXT NOT NULL REFERENCES device_ownership(id) ON DELETE CASCADE,
 membership_id TEXT NOT NULL REFERENCES memberships(id),
 permissions INTEGER NOT NULL CHECK(permissions>0),
 expires_at INTEGER NOT NULL,
 UNIQUE(ownership_id,membership_id)
);
PRAGMA user_version=5;`)
	return err
}
