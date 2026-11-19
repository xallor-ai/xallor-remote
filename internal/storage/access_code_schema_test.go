package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

// The current schema owns access-code lifecycle and resume records without rewriting device
// identity or the existing grant/share tables.
func TestSQLStore_shouldCreateAccessCodeLifecycleTables_atCurrentSchema(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenSQLStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var version int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 13 {
		t.Fatalf("schema version=%d", version)
	}
	for _, table := range []string{"access_codes", "code_consumptions", "session_authorizations"} {
		var count int
		if err := s.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&count); err != nil || count != 1 {
			t.Fatalf("table %s count=%d err=%v", table, count, err)
		}
	}
	var resumeCount int
	if err := s.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='resume_capabilities'`).Scan(&resumeCount); err != nil || resumeCount != 1 {
		t.Fatalf("resume_capabilities count=%d err=%v", resumeCount, err)
	}
	if _, err := s.db.ExecContext(context.Background(), `INSERT INTO access_codes (code_id,device_uid,ownership_id,type,secret_hash,issued_at,expires_at,generation,scope_json,require_approval,state) VALUES (?,?,?,?,?,?,?,?,?,?,?)`, "code", "device", "owner", "one_time", "hash", 100, nil, 1, `["info"]`, 1, "active"); err != nil {
		t.Fatal("access code insert:", err)
	}
	if _, err := s.db.Exec(`INSERT INTO code_consumptions (reservation_id,code_id,generation,session_id,state,reserved_at,reservation_expires_at,terminal_at) VALUES (?,?,?,?,?,?,?,?)`, "reservation", "code", 1, "session", "reserved", 100, 200, nil); err != nil {
		t.Fatal("consumption insert:", err)
	}
	if _, err := s.db.Exec(`INSERT INTO session_authorizations (session_id,code_id,generation,scope_json,issued_at,expires_at,state) VALUES (?,?,?,?,?,?,?)`, "session", "code", 1, `["info"]`, 100, nil, "active"); err != nil {
		t.Fatal("session insert:", err)
	}
}

// A failed schema upgrade must roll back the access-code DDL and version marker.
func TestSQLStore_shouldRollbackAccessCodeSchema_whenCreationConflicts(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "relay.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`PRAGMA user_version=8; CREATE VIEW access_codes AS SELECT 'conflict';`); err != nil {
		t.Fatal(err)
	}
	if s, err := OpenSQLStore(dir); err == nil {
		s.Close()
		t.Fatal("schema conflict accepted")
	}
	var version, tables int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name IN ('code_consumptions','session_authorizations')`).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if version != 8 || tables != 0 {
		t.Fatalf("partial access-code migration version=%d tables=%d", version, tables)
	}
}
