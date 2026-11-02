package relay

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/account"
)

// Account schema upgrades are atomic and retain the exact existing device/grant
// instances; reopening must preserve the authority and issued credentials.
func TestPrincipalSchema_should_preserveBindingsAndAuthority_whenUpgraded(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "relay.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`
CREATE TABLE devices (uid TEXT PRIMARY KEY NOT NULL, id TEXT UNIQUE NOT NULL, secret_hash TEXT NOT NULL, inbound INTEGER NOT NULL, last_seen INTEGER NOT NULL);
CREATE TABLE device_grants (id TEXT PRIMARY KEY NOT NULL, device_uid TEXT UNIQUE NOT NULL REFERENCES devices(uid) ON DELETE CASCADE, token_hash TEXT NOT NULL);
INSERT INTO devices VALUES ('uid','number','secret',1,123);
INSERT INTO device_grants VALUES ('grant-id','uid','grant-hash');
CREATE VIEW memberships AS SELECT 'migration-conflict';
PRAGMA user_version=3;`)
	if err != nil {
		t.Fatal(err)
	}
	if s, err := OpenSQLStore(dir); err == nil {
		s.Close()
		t.Fatal("malformed schema accepted")
	}
	var count, version int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name IN ('authority','principals','tenants')`).Scan(&count); err != nil || count != 0 {
		t.Fatal("partial upgrade escaped", count, err)
	}
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != 3 {
		t.Fatal("failed migration advanced schema", err)
	}
	if _, err := db.Exec(`DROP VIEW memberships`); err != nil {
		t.Fatal(err)
	}
	s, err := OpenSQLStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	device, err := s.LoadDevice(ctx, "number")
	if err != nil || device.UID != "uid" || device.GrantID != "grant-id" || device.GrantHash != "grant-hash" || device.SecretHash != "secret" || !device.Inbound {
		s.Close()
		t.Fatal("upgrade replaced device binding", device, err)
	}
	a, err := account.New(s).CreatePrincipal(ctx, account.CreatePrincipalRequest{Name: "alice", Kind: "user", TenantID: "", TTL: time.Hour})
	if err != nil {
		s.Close()
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenSQLStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, err := reopened.Authenticate(ctx, a.Token)
	if err != nil || got != a.Identity {
		t.Fatal("reopen replaced authority or membership", err)
	}
}
