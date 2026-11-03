package storage

import (
	"context"
	"database/sql"
	"github.com/xallor-ai/xallor-remote/internal/principal"
	"path/filepath"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/access"
)

// A schema-4 upgrade must preserve principal and device IDs and roll back every
// new table if DDL fails. Device removal cascades ownership and its shares.
func TestAccessSchema_should_preserveIdentityAndCascade_whenUpgraded(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	s, err := OpenSQLStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	a := enrollment(t, s, "alice", "")
	b := enrollment(t, s, "bob", "")
	device, err := s.RegisterDevice(ctx, "device", "secret", true)
	if err != nil {
		s.Close()
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`DROP TABLE device_shares; DROP TABLE device_ownership; PRAGMA user_version=4; CREATE VIEW device_shares AS SELECT 'conflict';`); err != nil {
		s.Close()
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if broken, err := OpenSQLStore(dir); err == nil {
		broken.Close()
		t.Fatal("DDL conflict accepted")
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "relay.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count, version int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name='device_ownership'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("partial ownership schema", err)
	}
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != 4 {
		t.Fatal("migration advanced version", err)
	}
	if _, err := db.Exec(`DROP VIEW device_shares`); err != nil {
		t.Fatal(err)
	}
	s, err = OpenSQLStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, err := principal.NewService(s).Authenticate(ctx, a.Token)
	if err != nil || p != a.Identity {
		t.Fatal("upgrade replaced principal", err)
	}
	current, err := s.LoadDevice(ctx, "device")
	if err != nil || current != device {
		t.Fatal("upgrade replaced device", err)
	}
	claimed := manageAccess(t, s, current, a.Identity, access.Request{Action: "claim"})
	manageAccess(t, s, claimed.Device, a.Identity, access.Request{Action: "share", MembershipID: b.MembershipID, Operations: []string{"info"}, TTLSeconds: 60})
	s.db.SetMaxIdleConns(0)
	if err := s.DeleteDevice(ctx, "device", device.UID, device.SecretHash); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow(`SELECT (SELECT count(*) FROM device_ownership)+(SELECT count(*) FROM device_shares)`).Scan(&count); err != nil || count != 0 {
		t.Fatal("cascade lost on replacement connection", count, err)
	}
	if _, err := principal.NewService(s).Authenticate(ctx, a.Token); err != nil {
		t.Fatal("device removal removed account", err)
	}
}
