package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/accesscode"
)

func insertManagementSibling(t *testing.T, s *SQLStore, id, uid string, generation uint64) {
	t.Helper()
	ctx := context.Background()
	target := accesscode.TargetBinding{DeviceUID: uid, Epoch: "resume-target"}
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		if uid != "device" {
			if err := tx.ActivateSessionTarget(ctx, target, resumeStorageNow); err != nil {
				return err
			}
		}
		code := accesscode.Code{ID: id, DeviceUID: uid, Generation: generation, Type: accesscode.LongLived, IssuedAt: resumeStorageNow, Scope: []string{"info"}, State: accesscode.StateActive}
		if err := tx.InsertCode(ctx, code, "hash"); err != nil {
			return err
		}
		if _, _, err := tx.ReserveCode(ctx, id, "hash", id+"-use", id+"-session", resumeStorageNow, resumeStorageNow.Add(time.Minute), target); err != nil {
			return err
		}
		if _, _, _, err := tx.FinalizeReservation(ctx, id+"-use", true, resumeStorageNow, nil, code.Scope); err != nil {
			return err
		}
		proof := resumeStorageFixture()
		proof.ID, proof.SessionID, proof.Generation = id+"-proof", id+"-session", generation
		return tx.InsertResume(ctx, proof)
	}); err != nil {
		t.Fatal(err)
	}
}

// Revoke is scoped to code/generation, not every code on the same device or
// another device. Active resume proofs on ended sessions also lose validity.
func TestCodeManagement_shouldScopeCascade_withoutTouchingSiblingGrants(t *testing.T) {
	s := codeManagementFixture(t)
	insertManagementSibling(t, s, "sibling", "device", 2)
	insertManagementSibling(t, s, "other-device", "other", 1)
	ctx := context.Background()
	if _, err := s.db.Exec(`UPDATE session_authorizations SET state='ended' WHERE session_id='resume-session'`); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error { _, err := tx.RevokeCode(ctx, "resume-code"); return err }); err != nil {
		t.Fatal(err)
	}
	if err := s.ReadAccessCode(ctx, func(r accesscode.Reader) error {
		p, err := r.Resume(ctx, resumeStorageFixture().ID)
		if err != nil {
			return err
		}
		if p.State != accesscode.ResumeRevoked {
			t.Error("ended session retained active proof")
		}
		for _, id := range []string{"sibling", "other-device"} {
			code, err := r.Code(ctx, id)
			if err != nil {
				return err
			}
			ss, err := r.Session(ctx, id+"-session")
			if err != nil {
				return err
			}
			p, err := r.Resume(ctx, id+"-proof")
			if err != nil {
				return err
			}
			if code.State != accesscode.StateActive || ss.State != accesscode.SessionActive || p.State != accesscode.ResumeActive {
				t.Error("unrelated family revoked")
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func managementMigrationFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	s := codeManagementFixtureAt(t, dir)
	insertManagementSibling(t, s, "legacy-revoked", "device", 2)
	insertManagementSibling(t, s, "used-code", "device", 3)
	if _, err := s.db.Exec(`UPDATE access_codes SET state='revoked' WHERE code_id='legacy-revoked';
UPDATE access_codes SET type='one_time',state='consumed' WHERE code_id='used-code';
INSERT INTO access_codes(code_id,device_uid,type,secret_hash,issued_at,generation,scope_json,require_approval,state)
VALUES('multi-unknown','device','one_time','hash',100,4,'["info"]',1,'active');
INSERT INTO code_consumptions(reservation_id,code_id,generation,session_id,state,exclusive,reserved_at,reservation_expires_at,target_uid,target_epoch)
VALUES('unknown-a','multi-unknown',4,'a','unknown',1,100,200,'device','resume-target'),('unknown-b','multi-unknown',4,'b','unknown',1,100,200,'device','resume-target');
DROP TABLE code_rotations;PRAGMA user_version=12;`); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	return dir
}

// v12→v13 must not rerun earlier blanket revocations. Only already-revoked
// legacy families are repaired, and repeated opens preserve current consent.
func TestCodeManagementMigration_shouldPreserveOrdinaryConsent_andRetireRevokedLegacyChildren(t *testing.T) {
	dir := managementMigrationFixture(t)
	for i := 0; i < 2; i++ {
		s, err := OpenSQLStore(dir)
		if err != nil {
			t.Fatal(err)
		}
		ctx := context.Background()
		err = s.ReadAccessCode(ctx, func(r accesscode.Reader) error {
			for id, want := range map[string]accesscode.SessionState{"resume-session": accesscode.SessionActive, "used-code-session": accesscode.SessionActive, "legacy-revoked-session": accesscode.SessionRevoked} {
				ss, err := r.Session(ctx, id)
				if err != nil {
					return err
				}
				if ss.State != want {
					t.Errorf("migration changed wrong consent: %s=%s", id, ss.State)
				}
			}
			for id, want := range map[string]accesscode.ReservationState{"resume-reservation": accesscode.ReservationConsumed, "used-code-use": accesscode.ReservationConsumed, "pending": accesscode.ReservationReserved, "unknown": accesscode.ReservationUnknown, "unknown-a": accesscode.ReservationUnknown, "unknown-b": accesscode.ReservationUnknown} {
				a, err := r.Reservation(ctx, id)
				if err != nil {
					return err
				}
				if a.State != want {
					t.Error("migration changed ordinary attempt")
				}
			}
			used, err := r.Code(ctx, "used-code")
			if err != nil {
				return err
			}
			if used.State != accesscode.StateConsumed {
				t.Error("migration refunded one-time code")
			}
			return nil
		})
		if err != nil {
			s.Close()
			t.Fatal(err)
		}
		if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
			return tx.RequireSessionTarget(ctx, accesscode.TargetBinding{DeviceUID: "device", Epoch: "resume-target"})
		}); err != nil {
			s.Close()
			t.Fatal("migration reset target", err)
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

// A late data-update fault rolls back receipt-table creation and schema marker,
// not only rows. Migration cannot report success after partial revocation.
func TestCodeManagementMigration_shouldRollbackDDLAndData_whenLegacyRepairFails(t *testing.T) {
	dir := managementMigrationFixture(t)
	db, err := sql.Open("sqlite", filepath.Join(dir, "relay.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TRIGGER deny_code_migration BEFORE UPDATE ON session_authorizations WHEN OLD.code_id='legacy-revoked' BEGIN
SELECT CASE WHEN (SELECT state FROM resume_capabilities WHERE resume_id='legacy-revoked-proof')='revoked'
THEN RAISE(ABORT,'fixture prior resume update observed') ELSE RAISE(ABORT,'fixture wrong update order') END; END;`); err != nil {
		t.Fatal(err)
	}
	if opened, err := OpenSQLStore(dir); err == nil {
		opened.Close()
		t.Fatal("failed migration accepted")
	} else if !strings.Contains(err.Error(), "fixture prior resume update observed") {
		t.Fatal("fixture did not fail after data update", err)
	}
	var version, tables int
	var state string
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name='code_rotations'`).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT state FROM session_authorizations WHERE session_id='legacy-revoked-session'`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if version != 12 || tables != 0 || state != "active" {
		t.Fatalf("partial migration: version=%d tables=%d state=%s", version, tables, state)
	}
	if err := db.QueryRow(`SELECT state FROM resume_capabilities WHERE resume_id='legacy-revoked-proof'`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "active" {
		t.Error("earlier resume update was not rolled back")
	}
}
