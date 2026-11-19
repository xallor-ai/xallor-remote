package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/accesscode"
)

// Rotation revokes retained consent; stale retirement is conditional and must
// preserve the replacement's new sessions and single-use resume proofs.
func TestSessionTargetStorage_shouldFenceOldEpoch_withoutRetiringReplacement(t *testing.T) {
	s, err := OpenSQLStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	insertResumeSession(t, s)
	proof := resumeStorageFixture()
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error { return tx.InsertResume(ctx, proof) }); err != nil {
		t.Fatal(err)
	}
	old := accesscode.TargetBinding{DeviceUID: "device", Epoch: "resume-target"}
	next := accesscode.TargetBinding{DeviceUID: "device", Epoch: "replacement"}
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		if err := tx.ActivateSessionTarget(ctx, next, resumeStorageNow); err != nil {
			return err
		}
		if _, _, err := tx.ReserveCode(ctx, "resume-code", "hash", "new-request", "new-session", resumeStorageNow, resumeStorageNow.Add(time.Minute), next); err != nil {
			return err
		}
		if _, _, _, err := tx.FinalizeReservation(ctx, "new-request", true, resumeStorageNow, nil, []string{"info"}); err != nil {
			return err
		}
		newProof := proof
		newProof.ID = "new-proof"
		newProof.SessionID = "new-session"
		return tx.InsertResume(ctx, newProof)
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error { return tx.RetireSessionTarget(ctx, old, resumeStorageNow) }); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		if err := tx.RequireSessionTarget(ctx, next); err != nil {
			return err
		}
		for _, item := range []struct {
			id, proof string
			state     accesscode.SessionState
			resume    accesscode.ResumeState
		}{{"resume-session", proof.ID, accesscode.SessionRevoked, accesscode.ResumeRevoked}, {"new-session", "new-proof", accesscode.SessionActive, accesscode.ResumeActive}} {
			got, err := tx.Session(ctx, item.id)
			if err != nil {
				return err
			}
			r, err := tx.Resume(ctx, item.proof)
			if err != nil {
				return err
			}
			if got.State != item.state || r.State != item.resume {
				t.Errorf("session=%s proof=%s", got.State, r.State)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		_, _, err := tx.ReserveCode(ctx, "resume-code", "hash", "stale", "stale-session", resumeStorageNow, resumeStorageNow.Add(time.Minute), old)
		return err
	}); !errors.Is(err, accesscode.ErrTargetEpoch) {
		t.Fatalf("old reserve accepted: %v", err)
	}
}

// Pending consent becomes unknown on target retirement. Neither a late approve
// nor a second redemption may recycle that one-time code's uncertain attempt.
func TestSessionTargetStorage_shouldKeepUnknownExclusive_whenRecipientIsRetired(t *testing.T) {
	s, err := OpenSQLStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	code := storageAccessCodeFixture()
	old := accesscode.TargetBinding{DeviceUID: code.DeviceUID, Epoch: "fixture-target"}
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		if err := insertTargetCodeFixture(ctx, tx, code, "hash"); err != nil {
			return err
		}
		_, _, err := tx.ReserveCode(ctx, code.ID, "hash", "pending", "pending-session", storageAccessCodeNow, storageAccessCodeNow.Add(time.Minute), old)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error { return tx.RetireSessionTarget(ctx, old, storageAccessCodeNow) }); err != nil {
		t.Fatal(err)
	}
	next := accesscode.TargetBinding{DeviceUID: code.DeviceUID, Epoch: "next"}
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		return tx.ActivateSessionTarget(ctx, next, storageAccessCodeNow)
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		_, _, _, err := tx.FinalizeReservation(ctx, "pending", true, storageAccessCodeNow, nil, code.Scope)
		return err
	}); !errors.Is(err, accesscode.ErrTargetEpoch) {
		t.Fatalf("stale approve accepted: %v", err)
	}
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		_, _, err := tx.ReserveCode(ctx, code.ID, "hash", "second", "second-session", storageAccessCodeNow, storageAccessCodeNow.Add(time.Minute), next)
		return err
	}); !errors.Is(err, accesscode.ErrReservationState) {
		t.Fatalf("unknown attempt was reusable: %v", err)
	}
	if err := s.ReadAccessCode(ctx, func(r accesscode.Reader) error {
		p, err := r.Reservation(ctx, "pending")
		if p.State != accesscode.ReservationUnknown {
			t.Error("lost unknown history")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

// Revocation and the active epoch switch must roll back together. A failed
// trigger cannot leave a new epoch while its old resume proof remains active.
func TestSessionTargetStorage_shouldRollbackEpoch_whenProofRevocationFails(t *testing.T) {
	s, err := OpenSQLStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	insertResumeSession(t, s)
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error { return tx.InsertResume(ctx, resumeStorageFixture()) }); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`CREATE TRIGGER deny_target_retire BEFORE UPDATE ON resume_capabilities BEGIN SELECT RAISE(ABORT,'fixture refusal'); END;`); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		return tx.ActivateSessionTarget(ctx, accesscode.TargetBinding{DeviceUID: "device", Epoch: "new"}, resumeStorageNow)
	}); err == nil {
		t.Fatal("failed rotation accepted")
	}
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		if err := tx.RequireSessionTarget(ctx, accesscode.TargetBinding{DeviceUID: "device", Epoch: "resume-target"}); err != nil {
			return err
		}
		ss, err := tx.Session(ctx, "resume-session")
		if ss.State != accesscode.SessionActive {
			t.Error("partial session revocation")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
