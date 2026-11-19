package storage

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/accesscode"
)

func rotateCodeForTest(ctx context.Context, tx accesscode.Transaction, oldID, newID, hash string, now time.Time) (accesscode.Code, error) {
	return tx.RotateCode(ctx, oldID, newID, hash, now)
}

func codeManagementFixture(t *testing.T) *SQLStore {
	t.Helper()
	return codeManagementFixtureAt(t, t.TempDir())
}

func codeManagementFixtureAt(t *testing.T, dir string) *SQLStore {
	t.Helper()
	s, err := OpenSQLStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	insertResumeSession(t, s)
	ctx := context.Background()
	target := accesscode.TargetBinding{DeviceUID: "device", Epoch: "resume-target"}
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		if err := tx.InsertResume(ctx, resumeStorageFixture()); err != nil {
			return err
		}
		if _, _, err := tx.ReserveCode(ctx, "resume-code", "hash", "pending", "pending-session", resumeStorageNow, resumeStorageNow.Add(time.Minute), target); err != nil {
			return err
		}
		if _, _, err := tx.ReserveCode(ctx, "resume-code", "hash", "unknown", "unknown-session", resumeStorageNow, resumeStorageNow.Add(time.Minute), target); err != nil {
			return err
		}
		_, err := tx.MarkReservationUnknown(ctx, "unknown", resumeStorageNow)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return s
}

// Explicit revocation cascades within the same transaction and is idempotent;
// unlike expiry, it disables already approved consent and its resume proof.
func TestCodeManagement_shouldRevokeDerivedConsent_withoutRefundingHistory(t *testing.T) {
	s := codeManagementFixture(t)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error { _, err := tx.RevokeCode(ctx, "resume-code"); return err }); err != nil {
			t.Errorf("revoke %d: %v", i, err)
		}
	}
	if err := s.ReadAccessCode(ctx, func(r accesscode.Reader) error {
		ss, err := r.Session(ctx, "resume-session")
		if err != nil {
			return err
		}
		if ss.State != accesscode.SessionRevoked {
			t.Errorf("session survived revoke: %s", ss.State)
		}
		proof, err := r.Resume(ctx, resumeStorageFixture().ID)
		if err != nil {
			return err
		}
		if proof.State != accesscode.ResumeRevoked {
			t.Errorf("resume survived revoke: %s", proof.State)
		}
		for id, want := range map[string]accesscode.ReservationState{"resume-reservation": accesscode.ReservationConsumed, "pending": accesscode.ReservationUnknown, "unknown": accesscode.ReservationUnknown} {
			got, err := r.Reservation(ctx, id)
			if err != nil {
				return err
			}
			if got.State != want {
				t.Errorf("history %s=%s want=%s", id, got.State, want)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// Revoking a used one-time code must remain possible without refunding its
// consumption or retaining the session it previously authorized.
func TestCodeManagement_shouldRevokeConsumedCode_withoutRestoringEligibility(t *testing.T) {
	s := codeManagementFixture(t)
	ctx := context.Background()
	if _, err := s.db.Exec(`UPDATE access_codes SET type='one_time',state='consumed' WHERE code_id='resume-code'`); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error { _, err := tx.RevokeCode(ctx, "resume-code"); return err }); err != nil {
		t.Fatal(err)
	}
	if err := s.ReadAccessCode(ctx, func(r accesscode.Reader) error {
		record, err := r.Reservation(ctx, "resume-reservation")
		if record.State != accesscode.ReservationConsumed {
			t.Error("one-time use refunded")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

// A retry must query the original replacement, with its original issuance time
// and current state, rather than creating another generation or reactivating it.
func TestCodeManagement_shouldRotateOnce_andReturnReceiptOnRetry(t *testing.T) {
	s := codeManagementFixture(t)
	ctx := context.Background()
	var first accesscode.Code
	hash := strings.Repeat("b", 64)
	issued := resumeStorageNow.Add(10 * time.Second)
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		var err error
		first, err = rotateCodeForTest(ctx, tx, "resume-code", "replacement", hash, issued)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if first.ID != "replacement" || first.Generation != 2 || first.DeviceUID != "device" || first.State != accesscode.StateActive {
		t.Fatalf("bad replacement: %+v", first)
	}
	if err := s.ReadAccessCode(ctx, func(r accesscode.Reader) error {
		pending, err := r.Reservation(ctx, "pending")
		if err != nil {
			return err
		}
		if pending.TerminalAt == nil || !pending.TerminalAt.Equal(first.IssuedAt) {
			t.Errorf("rotation timestamps diverged: terminal=%v issued=%v", pending.TerminalAt, first.IssuedAt)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error { _, err := tx.RevokeCode(ctx, first.ID); return err }); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		again, err := rotateCodeForTest(ctx, tx, "resume-code", "replacement", hash, issued.Add(time.Hour))
		if err == nil && (again.State != accesscode.StateRevoked || !again.IssuedAt.Equal(first.IssuedAt)) {
			t.Error("retry reissued/revived replacement")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		_, err := rotateCodeForTest(ctx, tx, "resume-code", "different", hash, issued)
		return err
	}); err == nil {
		t.Error("conflicting intent accepted")
	}
}
