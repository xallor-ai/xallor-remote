package relay

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/access"
)

// Opening a populated schema-1 database removes operation payload storage while
// preserving its device credentials and inbound state, including after reopening.
func TestSQLStore_should_preserveIdentityOnly_whenRemovingOperationHistory(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "relay.db"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE devices (id TEXT PRIMARY KEY, secret_hash TEXT NOT NULL, grant_hash TEXT NOT NULL DEFAULT '', inbound INTEGER NOT NULL DEFAULT 0, last_seen INTEGER NOT NULL DEFAULT 0);
INSERT INTO devices VALUES ('device','secret-hash','grant-hash',1,1234);
CREATE TABLE audit (ts INTEGER, args_preview TEXT, args_digest TEXT);
CREATE INDEX audit_ts ON audit(ts);
INSERT INTO audit VALUES (1234,'private-command-argument','private-digest');
PRAGMA user_version=1;`)
	_ = db.Close()
	if err != nil {
		t.Fatal(err)
	}
	var binding access.Binding
	for run := range 2 {
		s, err := OpenSQLStore(dir)
		if err != nil {
			t.Fatal(err)
		}
		func() {
			defer s.Close()
			r, err := s.LoadDevice(context.Background(), "device")
			if err != nil || r.SecretHash != "secret-hash" || r.GrantHash != "grant-hash" || !r.Inbound || r.UID == "" || r.GrantID == "" {
				t.Fatalf("identity changed: %+v %v", r, err)
			}
			if run == 0 {
				binding = r.Binding()
			} else if !binding.Matches(r.Binding()) {
				t.Fatal("reopening changed the migrated binding")
			}
			var tables string
			if err := s.db.QueryRow(`SELECT group_concat(name) FROM (SELECT name FROM sqlite_master WHERE type='table' ORDER BY name)`).Scan(&tables); err != nil || tables != "device_grants,devices" {
				t.Fatalf("unexpected persisted collections: %q %v", tables, err)
			}
			var indexes, seen, version int
			if err := s.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name='audit_ts'`).Scan(&indexes); err != nil {
				t.Fatal(err)
			}
			if err := s.db.QueryRow(`SELECT last_seen FROM devices WHERE id='device'`).Scan(&seen); err != nil {
				t.Fatal(err)
			}
			if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
				t.Fatal(err)
			}
			if indexes != 0 || seen != 1234 || version != 3 {
				t.Fatalf("unexpected schema/state: indexes=%d last_seen=%d version=%d", indexes, seen, version)
			}
		}()
	}
}
