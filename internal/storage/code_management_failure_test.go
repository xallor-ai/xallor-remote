package storage

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/accesscode"
)

// Faults after revocation, child invalidation or replacement insertion must
// roll back the entire rotation, preserving the original grant and history.
func TestCodeManagement_shouldRollbackWholeRotation_whenAnyWriteFails(t *testing.T) {
	for _, stage := range []string{"cascade", "replacement", "receipt"} {
		t.Run(stage, func(t *testing.T) {
			s := codeManagementFixture(t)
			ctx := context.Background()
			trigger := `CREATE TRIGGER fail_rotation BEFORE UPDATE ON resume_capabilities BEGIN SELECT RAISE(ABORT,'fixture cascade refusal'); END;`
			if stage == "replacement" {
				trigger = `CREATE TRIGGER fail_rotation BEFORE INSERT ON access_codes WHEN NEW.code_id='replacement' BEGIN SELECT RAISE(ABORT,'fixture insert refusal'); END;`
			}
			if stage == "receipt" {
				trigger = `CREATE TRIGGER fail_rotation BEFORE INSERT ON code_rotations BEGIN SELECT RAISE(ABORT,'fixture receipt refusal'); END;`
			}
			if _, err := s.db.Exec(trigger); err != nil {
				t.Fatal(err)
			}
			if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
				_, err := tx.RotateCode(ctx, "resume-code", "replacement", strings.Repeat("b", 64), resumeStorageNow)
				return err
			}); err == nil {
				t.Fatal("fault accepted")
			}
			if err := s.ReadAccessCode(ctx, func(r accesscode.Reader) error {
				code, err := r.Code(ctx, "resume-code")
				if err != nil {
					return err
				}
				ss, err := r.Session(ctx, "resume-session")
				if err != nil {
					return err
				}
				proof, err := r.Resume(ctx, resumeStorageFixture().ID)
				if err != nil {
					return err
				}
				pending, err := r.Reservation(ctx, "pending")
				if err != nil {
					return err
				}
				if code.State != accesscode.StateActive || ss.State != accesscode.SessionActive || proof.State != accesscode.ResumeActive || pending.State != accesscode.ReservationReserved {
					t.Error("partial rotation remained")
				}
				if _, err := r.Rotation(ctx, "resume-code"); !errors.Is(err, accesscode.ErrRotationNotFound) {
					t.Error("failed rotation recorded receipt")
				}
				if _, err := r.Code(ctx, "replacement"); err == nil {
					t.Error("failed rotation retained replacement")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := s.db.Exec(`DROP TRIGGER fail_rotation`); err != nil {
				t.Fatal(err)
			}
			if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
				_, err := tx.RotateCode(ctx, "resume-code", "replacement", strings.Repeat("b", 64), resumeStorageNow)
				return err
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// Natural expiry is not explicit revocation: approved consent/resume retain
// their independent deadline, but a later explicit revoke invalidates them.
func TestCodeManagement_shouldPreserveSessionAfterCodeExpiry_untilExplicitRevoke(t *testing.T) {
	s, code, target := unusedRotationFixture(t, accesscode.Expiring)
	ctx := context.Background()
	expires := resumeStorageNow.Add(10 * time.Minute)
	proof := resumeStorageFixture()
	proof.SessionID = "expiry-session"
	proof.ExpiresAt = expires
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		if _, _, err := tx.ReserveCode(ctx, code.ID, strings.Repeat("a", 64), "expiry-use", proof.SessionID, resumeStorageNow, *code.ExpiresAt, target); err != nil {
			return err
		}
		if _, _, _, err := tx.FinalizeReservation(ctx, "expiry-use", true, resumeStorageNow, &expires, code.Scope); err != nil {
			return err
		}
		return tx.InsertResume(ctx, proof)
	}); err != nil {
		t.Fatal(err)
	}
	later := code.ExpiresAt.Add(time.Second)
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		_, _, err := tx.ReserveCode(ctx, code.ID, strings.Repeat("a", 64), "too-late", "new-session", later, later.Add(time.Minute), target)
		return err
	}); !errors.Is(err, accesscode.ErrCodeExpired) {
		t.Fatal("expired code admitted", err)
	}
	newProof := proof
	newProof.ID, newProof.IssuedAt = "new-proof", later
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		_, err := tx.ConsumeResume(ctx, proof.ID, proof.CapabilityHash, later, proof.SessionID, proof.BindingKind, proof.BindingDigest)
		if err != nil {
			return err
		}
		return tx.InsertResume(ctx, newProof)
	}); err != nil {
		t.Fatal("code expiry killed approved resume", err)
	}
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error { _, err := tx.RevokeCode(ctx, code.ID); return err }); err != nil {
		t.Fatal(err)
	}
	if err := s.ReadAccessCode(ctx, func(r accesscode.Reader) error {
		ss, err := r.Session(ctx, proof.SessionID)
		if err != nil {
			return err
		}
		if ss.State != accesscode.SessionRevoked {
			t.Error("explicit revoke left consent active")
		}
		p, err := r.Resume(ctx, newProof.ID)
		if p.State != accesscode.ResumeRevoked {
			t.Error("explicit revoke left resume active")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
