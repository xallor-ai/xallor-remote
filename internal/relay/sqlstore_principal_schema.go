package relay

import "database/sql"

func migratePrincipals(tx *sql.Tx) error {
	_, err := tx.Exec(`
CREATE TABLE authority (singleton INTEGER PRIMARY KEY CHECK(singleton=1), id TEXT NOT NULL UNIQUE);
INSERT INTO authority VALUES (1,lower(hex(randomblob(16))));
CREATE TABLE principals (id TEXT PRIMARY KEY NOT NULL, kind TEXT NOT NULL CHECK(kind IN ('user','service')), name TEXT NOT NULL);
CREATE TABLE tenants (id TEXT PRIMARY KEY NOT NULL, kind TEXT NOT NULL CHECK(kind IN ('personal','organization')), name TEXT NOT NULL);
CREATE TABLE memberships (
 id TEXT PRIMARY KEY NOT NULL,
 tenant_id TEXT NOT NULL REFERENCES tenants(id),
 principal_id TEXT NOT NULL REFERENCES principals(id),
 role TEXT NOT NULL CHECK(role IN ('owner','member')),
 active INTEGER NOT NULL DEFAULT 1,
 version INTEGER NOT NULL DEFAULT 1,
 UNIQUE(tenant_id,principal_id)
);
CREATE TABLE principal_credentials (
 id TEXT PRIMARY KEY NOT NULL,
 token_hash TEXT NOT NULL UNIQUE,
 membership_id TEXT NOT NULL REFERENCES memberships(id),
 expires_at INTEGER NOT NULL,
 revoked INTEGER NOT NULL DEFAULT 0
);
PRAGMA user_version=4;`)
	return err
}
