package storage

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/accesscode"
)

// Produce the actual predecessor table shape, not only a downgraded version
// marker on new tables. The retained rows have no recipient epoch evidence.
func targetLegacyFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	s, err := OpenSQLStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	insertResumeSession(t, s)
	if err := s.WriteAccessCode(context.Background(), func(tx accesscode.Transaction) error {
		return tx.InsertResume(context.Background(), resumeStorageFixture())
	}); err != nil {
		t.Fatal(err)
	}
	_, err = s.db.Exec(`
UPDATE access_codes SET type='one_time',state='consumed';
INSERT INTO access_codes(code_id,device_uid,ownership_id,type,secret_hash,issued_at,generation,scope_json,require_approval,state)
VALUES ('legacy-unknown','unknown-device','','one_time','hash',1,1,'["info"]',1,'active');
INSERT INTO code_consumptions(reservation_id,code_id,generation,session_id,state,exclusive,reserved_at,reservation_expires_at)
VALUES ('u1','legacy-unknown',1,'s1','unknown',1,1,9999999999),('u2','legacy-unknown',1,'s2','unknown',1,1,9999999999),('r1','legacy-unknown',1,'s3','reserved',1,1,9999999999);
DROP INDEX session_target_idx; DROP INDEX reservation_target_idx;
ALTER TABLE code_consumptions DROP COLUMN target_uid;
ALTER TABLE code_consumptions DROP COLUMN target_epoch;
ALTER TABLE session_authorizations DROP COLUMN target_uid;
ALTER TABLE session_authorizations DROP COLUMN target_epoch;
DROP TABLE session_targets;
PRAGMA user_version=11;`)
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// Multiple historical unknown attempts must neither break migration nor be
// released to satisfy a new uniqueness rule. Consumed codes remain consumed.
func TestTargetMigration_shouldPreserveUnknownHistory_withoutRefund(t *testing.T) {
	dir := targetLegacyFixture(t)
	s, err := OpenSQLStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := s.ReadAccessCode(ctx, func(r accesscode.Reader) error {
		code, err := r.Code(ctx, "resume-code")
		if err != nil {
			return err
		}
		if code.State != accesscode.StateConsumed {
			t.Error("consumption refunded")
		}
		session, err := r.Session(ctx, "resume-session")
		if err != nil {
			return err
		}
		if session.State != accesscode.SessionRevoked {
			t.Error("legacy consent survived")
		}
		proof, err := r.Resume(ctx, resumeStorageFixture().ID)
		if err != nil {
			return err
		}
		if proof.State != accesscode.ResumeRevoked {
			t.Error("legacy proof survived")
		}
		for _, id := range []string{"u1", "u2", "r1"} {
			r, err := r.Reservation(ctx, id)
			if err != nil {
				return err
			}
			if r.State != accesscode.ReservationUnknown {
				t.Errorf("attempt %s was released/lost", id)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	target := accesscode.TargetBinding{DeviceUID: "unknown-device", Epoch: "after-migration"}
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error { return tx.ActivateSessionTarget(ctx, target, time.Now()) }); err != nil {
		t.Fatal(err)
	}
	err = s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		_, _, err := tx.ReserveCode(ctx, "legacy-unknown", "hash", "new", "new-session", time.Now(), time.Now().Add(time.Minute), target)
		return err
	})
	if !errors.Is(err, accesscode.ErrReservationState) {
		t.Fatalf("legacy unknown lost exclusivity: %v", err)
	}
}

// A late migration failure must roll back earlier revocations, DDL and the
// schema marker together; malformed target storage must never be accepted.
func TestTargetMigration_shouldRollbackAllChanges_whenTargetObjectIsInvalid(t *testing.T) {
	dir := targetLegacyFixture(t)
	db, err := sql.Open("sqlite", filepath.Join(dir, "relay.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE VIEW session_targets AS SELECT 'invalid' AS epoch;`); err != nil {
		t.Fatal(err)
	}
	if opened, err := OpenSQLStore(dir); err == nil {
		opened.Close()
		t.Fatal("invalid target schema accepted")
	}
	var version, columns int
	var state string
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM pragma_table_info('session_authorizations') WHERE name='target_epoch'`).Scan(&columns); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT state FROM session_authorizations WHERE session_id='resume-session'`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if version != 11 || columns != 0 || state != "active" {
		t.Fatalf("partial migration: version=%d columns=%d state=%s", version, columns, state)
	}
}
