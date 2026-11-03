package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

// Schema-2 device hashes migrate once into grant rows. An ungranted device must
// remain ungranted, and foreign-key protection must survive pool replacement.
func TestSQLAccess_should_migrateAndEnforceRelation_whenOpeningSchemaTwo(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "relay.db"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE devices (id TEXT PRIMARY KEY, secret_hash TEXT NOT NULL, grant_hash TEXT NOT NULL DEFAULT '', inbound INTEGER NOT NULL DEFAULT 0, last_seen INTEGER NOT NULL DEFAULT 0);
INSERT INTO devices VALUES ('paired','secret','token',1,1234),('unpaired','other','',0,0);
PRAGMA user_version=2;`)
	_ = db.Close()
	if err != nil {
		t.Fatal(err)
	}
	s, err := OpenSQLStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	paired, err := s.LoadDevice(ctx, "paired")
	if err != nil || paired.UID == "" || paired.GrantID == "" || paired.GrantHash != "token" {
		t.Fatalf("paired device: %+v %v", paired, err)
	}
	unpaired, err := s.LoadDevice(ctx, "unpaired")
	if err != nil || unpaired.UID == "" || unpaired.GrantID != "" || unpaired.GrantHash != "" || unpaired.UID == paired.UID {
		t.Fatalf("unpaired device: %+v %v", unpaired, err)
	}
	// Close all idle connections so every statement uses a fresh configured one.
	s.db.SetMaxIdleConns(0)
	if _, err := s.db.Exec(`INSERT INTO device_grants (id,device_uid,token_hash) VALUES ('forged','missing','token')`); err == nil {
		t.Fatal("new pooled connection allowed an orphan grant")
	}
	if err := s.DeleteDevice(ctx, "paired", paired.UID, paired.SecretHash); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := s.db.QueryRow(`SELECT count(*) FROM device_grants`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("cascade was lost after connection replacement: %d %v", count, err)
	}
	if _, err := s.db.Query(`SELECT grant_hash FROM devices`); err == nil {
		t.Fatal("retained a duplicate grant column")
	}
}
