package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/accesscode"
)

// Schema 10 did not preserve recipient scope in active authorizations. Upgrade
// must revoke those sessions/resume proofs without refunding consumed codes.
func TestConsentMigration_shouldRevokeUnprovableLegacySessions_withoutRefund(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenSQLStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	insertResumeSession(t, s)
	resume := resumeStorageFixture()
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error { return tx.InsertResume(ctx, resume) }); err != nil {
		s.Close()
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE session_authorizations SET scope_json='["info","exec"]'; UPDATE access_codes SET type='one_time',state='consumed'; PRAGMA user_version=10;`); err != nil {
		s.Close()
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = OpenSQLStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.ReadAccessCode(ctx, func(r accesscode.Reader) error {
		session, err := r.Session(ctx, resume.SessionID)
		if err != nil {
			return err
		}
		if session.State != accesscode.SessionRevoked {
			t.Error("legacy active session survived upgrade")
		}
		capability, err := r.Resume(ctx, resume.ID)
		if err != nil {
			return err
		}
		if capability.State != accesscode.ResumeRevoked {
			t.Error("legacy resume proof survived upgrade")
		}
		code, err := r.Code(ctx, session.CodeID)
		if err != nil {
			return err
		}
		if code.State != accesscode.StateConsumed {
			t.Error("migration refunded one-time code")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// Once schema 11 has enforced the consent boundary, reopening must preserve a
// newly approved session instead of revoking every session on every startup.
func TestConsentMigration_shouldPreserveNewSessions_onRepeatedOpen(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenSQLStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	insertResumeSession(t, s)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = OpenSQLStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.ReadAccessCode(context.Background(), func(r accesscode.Reader) error {
		session, err := r.Session(context.Background(), "resume-session")
		if err != nil {
			return err
		}
		return session.CanOperate(resumeStorageNow.Add(2 * time.Second))
	}); err != nil {
		t.Fatal(err)
	}
}

// A failed capability invalidation must roll back the session invalidation and
// schema marker too; a partially upgraded authorization database is unusable.
func TestConsentMigration_shouldRollbackAllChanges_whenResumeUpdateFails(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenSQLStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	insertResumeSession(t, s)
	if err := s.WriteAccessCode(context.Background(), func(tx accesscode.Transaction) error {
		return tx.InsertResume(context.Background(), resumeStorageFixture())
	}); err != nil {
		s.Close()
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`PRAGMA user_version=10; CREATE TRIGGER deny_resume_migration BEFORE UPDATE ON resume_capabilities BEGIN SELECT RAISE(ABORT,'fixture migration failure'); END;`); err != nil {
		s.Close()
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if opened, err := OpenSQLStore(dir); err == nil {
		opened.Close()
		t.Fatal("failed migration reported success")
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "relay.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version int
	var sessionState, resumeState string
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT state FROM session_authorizations WHERE session_id='resume-session'`).Scan(&sessionState); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT state FROM resume_capabilities WHERE resume_id='resume-1'`).Scan(&resumeState); err != nil {
		t.Fatal(err)
	}
	if version != 10 || sessionState != "active" || resumeState != "active" {
		t.Fatalf("partial migration: version=%d session=%s resume=%s", version, sessionState, resumeState)
	}
}
