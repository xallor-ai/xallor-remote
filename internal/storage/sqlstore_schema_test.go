package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

// Schema initialization removes the redundant grant mirror without changing the authoritative device identity.
func TestSQLStore_should_preserveIdentity_whenSchemaNormalized(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "relay.db"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE devices (id TEXT PRIMARY KEY, secret_hash TEXT NOT NULL, grant_hash TEXT NOT NULL DEFAULT '', inbound INTEGER NOT NULL DEFAULT 0, last_seen INTEGER NOT NULL DEFAULT 0);
INSERT INTO devices VALUES ('device','secret','current-grant',1,0);
CREATE TABLE grants (device_id TEXT PRIMARY KEY, grant_hash TEXT NOT NULL, updated_at INTEGER NOT NULL);
INSERT INTO grants VALUES ('device','stale-mirror',0);`)
	_ = db.Close()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		s, err := OpenSQLStore(dir)
		if err != nil {
			t.Fatal(err)
		}
		r, err := s.LoadDevice(context.Background(), "device")
		if err != nil || r.SecretHash != "secret" || r.GrantHash != "current-grant" || !r.Inbound || r.UID == "" || r.GrantID == "" {
			t.Fatalf("identity: %+v %v", r, err)
		}
		var count, version int
		if err := s.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='grants'`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
			t.Fatal(err)
		}
		if count != 0 || version != 6 {
			t.Fatalf("schema count=%d version=%d", count, version)
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

// An unsupported database version must fail startup without overwriting its version.
func TestSQLStore_should_rejectNewerSchema(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "relay.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`PRAGMA user_version=99`); err != nil {
		t.Fatal(err)
	}
	if s, err := OpenSQLStore(dir); err == nil {
		s.Close()
		t.Fatal("accepted unsupported schema")
	}
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != 99 {
		t.Fatalf("version=%d error=%v", version, err)
	}
}

// A schema failure after a DROP must roll back both DDL and the version marker.
func TestSQLStore_should_rollbackSchema_whenInitializationFails(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "relay.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE grants (device_id TEXT); INSERT INTO grants VALUES ('fixture'); CREATE VIEW audit AS SELECT 'fixture';`); err != nil {
		t.Fatal(err)
	}
	if s, err := OpenSQLStore(dir); err == nil {
		s.Close()
		t.Fatal("accepted malformed schema")
	}
	var count, version int
	if err := db.QueryRow(`SELECT count(*) FROM grants`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if count != 1 || version != 0 {
		t.Fatalf("partial schema committed: count=%d version=%d", count, version)
	}
}
