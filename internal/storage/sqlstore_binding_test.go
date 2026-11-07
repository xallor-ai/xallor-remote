package storage

import (
	"context"
	"errors"
	"github.com/xallor-ai/xallor-remote/internal/access"
	"testing"
)

// UID and grant binding survive reconnects and idempotent synchronization, while
// an old snapshot cannot mutate a recreated identity even with the same secret.
func TestSQLAccess_should_rejectStaleIdentity_whenDisplayNumberReused(t *testing.T) {
	s := openIdentityTestStore(t)
	ctx := context.Background()
	first, err := s.RegisterDevice(ctx, "number", "same-secret", true)
	if err != nil {
		t.Fatal(err)
	}
	first = setTestAccess(t, s, "number", "grant", true)
	again, err := s.RegisterDevice(ctx, "number", "same-secret", false)
	if err != nil || again != first {
		t.Fatalf("reconnection changed identity/access: %+v %v", again, err)
	}
	again.GrantID = "caller-cannot-choose-an-authorization-id"
	again, err = s.UpdateDevice(ctx, "number", again)
	if err != nil || again != first {
		t.Fatalf("caller replaced trusted binding: %+v %v", again, err)
	}
	if err := s.DeleteDevice(ctx, "number", first.UID, first.SecretHash); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := s.db.QueryRow(`SELECT count(*) FROM device_grants`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("deleted device retained its grant: %d %v", count, err)
	}
	replacement, err := s.RegisterDevice(ctx, "number", "same-secret", true)
	if err != nil || replacement.UID == first.UID {
		t.Fatalf("recreated identity reused UID: %+v %v", replacement, err)
	}
	if _, err := s.UpdateDevice(ctx, "number", first); !errors.Is(err, access.ErrDeviceIdentity) {
		t.Fatalf("stale update accepted: %v", err)
	}
	if err := s.DeleteDevice(ctx, "number", first.UID, first.SecretHash); !errors.Is(err, access.ErrDeviceIdentity) {
		t.Fatalf("stale deletion accepted: %v", err)
	}
	got, err := s.LoadDevice(ctx, "number")
	if err != nil || got != replacement {
		t.Fatalf("replacement changed: %+v %v", got, err)
	}
}

// A failure in the second table must roll back the device change and retain the
// previous committed grant ID, rather than publish a partially rotated identity.
func TestSQLAccess_should_rollbackDeviceChange_whenGrantWriteFails(t *testing.T) {
	s := openIdentityTestStore(t)
	ctx := context.Background()
	if _, err := s.RegisterDevice(ctx, "number", "secret", true); err != nil {
		t.Fatal(err)
	}
	before := setTestAccess(t, s, "number", "old", true)
	if _, err := s.db.Exec(`CREATE TRIGGER reject_grant BEFORE INSERT ON device_grants BEGIN SELECT RAISE(ABORT,'fixture'); END;`); err != nil {
		t.Fatal(err)
	}
	change := before
	change.Inbound, change.GrantHash = false, "new"
	if _, err := s.UpdateDevice(ctx, "number", change); err == nil {
		t.Fatal("grant failure was ignored")
	}
	after, err := s.LoadDevice(ctx, "number")
	if err != nil || after != before {
		t.Fatalf("partial transaction escaped: %+v %v", after, err)
	}
}
