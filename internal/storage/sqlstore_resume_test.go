package storage

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/accesscode"
)

var resumeStorageNow = time.Date(2026, 11, 18, 13, 0, 0, 0, time.UTC)

func insertResumeSession(t *testing.T, s *SQLStore) {
	t.Helper()
	ctx := context.Background()
	code := accesscode.Code{ID: "resume-code", DeviceUID: "device", Type: accesscode.LongLived, Generation: 1, IssuedAt: resumeStorageNow, Scope: []string{"info"}, State: accesscode.StateActive}
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		if err := tx.ActivateSessionTarget(ctx, accesscode.TargetBinding{DeviceUID: code.DeviceUID, Epoch: "resume-target"}, resumeStorageNow); err != nil {
			return err
		}
		if err := tx.InsertCode(ctx, code, "hash"); err != nil {
			return err
		}
		if _, _, err := tx.ReserveCode(ctx, code.ID, "hash", "resume-reservation", "resume-session", resumeStorageNow, resumeStorageNow.Add(time.Minute), accesscode.TargetBinding{DeviceUID: code.DeviceUID, Epoch: "resume-target"}); err != nil {
			return err
		}
		_, _, session, err := tx.FinalizeReservation(ctx, "resume-reservation", true, resumeStorageNow.Add(time.Second), nil, code.Scope)
		if err != nil || session.State != accesscode.SessionActive {
			return err
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func resumeStorageFixture() accesscode.ResumeCapability {
	return accesscode.ResumeCapability{ID: "resume-1", SessionID: "resume-session", BindingKind: "device", BindingDigest: strings.Repeat("b", 64), CapabilityHash: strings.Repeat("a", 64), IssuedAt: resumeStorageNow, ExpiresAt: resumeStorageNow.Add(time.Minute), Generation: 1, State: accesscode.ResumeActive}
}

// A rejected proof must leave the capability active even if the caller commits
// other transaction work; authentication precedes the conditional state update.
func TestSQLResume_shouldRejectWrongHash_beforeConsumption(t *testing.T) {
	s, err := OpenSQLStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	insertResumeSession(t, s)
	resume := resumeStorageFixture()
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error { return tx.InsertResume(ctx, resume) }); err != nil {
		t.Fatal(err)
	}
	for _, hash := range []string{"", "not-a-digest", strings.Repeat("c", 64)} {
		if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
			_, err := tx.ConsumeResume(ctx, resume.ID, hash, resumeStorageNow.Add(time.Second), resume.SessionID, resume.BindingKind, resume.BindingDigest)
			if !errors.Is(err, accesscode.ErrResumeSecret) {
				t.Fatalf("wrong proof error=%v", err)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.ReadAccessCode(ctx, func(r accesscode.Reader) error {
		got, err := r.Resume(ctx, resume.ID)
		if err != nil {
			return err
		}
		if got.State != accesscode.ResumeActive {
			t.Fatal("rejected proof changed capability state")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestSQLResume_shouldConsumeAtomically_andRejectReuse(t *testing.T) {
	s, err := OpenSQLStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	insertResumeSession(t, s)
	resume := resumeStorageFixture()
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error { return tx.InsertResume(ctx, resume) }); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		got, err := tx.ConsumeResume(ctx, resume.ID, resume.CapabilityHash, resumeStorageNow.Add(time.Second), resume.SessionID, resume.BindingKind, resume.BindingDigest)
		if err != nil || got.State != accesscode.ResumeConsumed {
			t.Fatalf("consumed=%+v err=%v", got, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		_, err := tx.ConsumeResume(ctx, resume.ID, resume.CapabilityHash, resumeStorageNow.Add(2*time.Second), resume.SessionID, resume.BindingKind, resume.BindingDigest)
		if !errors.Is(err, accesscode.ErrResumeConsumed) {
			t.Fatalf("reuse error=%v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestSQLResume_shouldPersistUnknownAndRevokeTransitions(t *testing.T) {
	s, err := OpenSQLStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	insertResumeSession(t, s)
	for _, id := range []string{"resume-unknown", "resume-revoke"} {
		resume := resumeStorageFixture()
		resume.ID = id
		if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error { return tx.InsertResume(ctx, resume) }); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		if _, err := tx.MarkResumeUnknown(ctx, "resume-unknown", resumeStorageNow.Add(time.Second)); err != nil {
			return err
		}
		_, err := tx.RevokeResume(ctx, "resume-revoke", resumeStorageNow.Add(time.Second))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReadAccessCode(ctx, func(reader accesscode.Reader) error {
		unknown, err := reader.Resume(ctx, "resume-unknown")
		if err != nil || unknown.State != accesscode.ResumeUnknown {
			t.Fatalf("unknown=%+v err=%v", unknown, err)
		}
		revoked, err := reader.Resume(ctx, "resume-revoke")
		if err != nil || revoked.State != accesscode.ResumeRevoked {
			t.Fatalf("revoked=%+v err=%v", revoked, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestSQLResume_shouldRevokeWithSessionAuthorization(t *testing.T) {
	s, err := OpenSQLStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	insertResumeSession(t, s)
	resume := resumeStorageFixture()
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error { return tx.InsertResume(ctx, resume) }); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		_, err := tx.RevokeSession(ctx, resume.SessionID, resumeStorageNow.Add(time.Second))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReadAccessCode(ctx, func(reader accesscode.Reader) error {
		got, err := reader.Resume(ctx, resume.ID)
		if err != nil || got.State != accesscode.ResumeRevoked {
			t.Fatalf("resume=%+v err=%v", got, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
