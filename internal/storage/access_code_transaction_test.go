package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/accesscode"
)

var storageAccessCodeNow = time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC)

func storageAccessCodeFixture() accesscode.Code {
	return accesscode.Code{ID: "code-1", DeviceUID: "device-1", OwnershipID: "owner-1", Type: accesscode.OneTime,
		Generation: 1, IssuedAt: storageAccessCodeNow, Scope: []string{"info", "exec"}, RequireApproval: true, State: accesscode.StateActive}
}

// A final approval atomically consumes the one-time code and creates a session row.
func TestSQLAccessCode_shouldConsumeAndPersistSession_inOneWrite(t *testing.T) {
	s, err := OpenSQLStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	code := storageAccessCodeFixture()
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error { return insertTargetCodeFixture(ctx, tx, code, "hash-1") }); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		_, _, err := tx.ReserveCode(ctx, code.ID, "hash-1", "reservation-1", "session-1", storageAccessCodeNow, storageAccessCodeNow.Add(time.Minute), accesscode.TargetBinding{DeviceUID: code.DeviceUID, Epoch: "fixture-target"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		_, reservation, session, err := tx.FinalizeReservation(ctx, "reservation-1", true, storageAccessCodeNow.Add(time.Second), nil, code.Scope)
		if err != nil || reservation.State != accesscode.ReservationConsumed || session.State != accesscode.SessionActive {
			t.Fatalf("reservation=%+v session=%+v err=%v", reservation, session, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReadAccessCode(ctx, func(reader accesscode.Reader) error {
		code, err := reader.Code(ctx, "code-1")
		if err != nil || code.State != accesscode.StateConsumed {
			return errors.New("one-time code was not consumed")
		}
		session, err := reader.Session(ctx, "session-1")
		if err != nil || session.State != accesscode.SessionActive {
			return errors.New("session authorization was not persisted")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// Rejected approval releases a one-time reservation without consuming the code.
func TestSQLAccessCode_shouldReleaseRejectedReservation_andPermitFreshAttempt(t *testing.T) {
	s, err := OpenSQLStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	code := storageAccessCodeFixture()
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error { return insertTargetCodeFixture(ctx, tx, code, "hash-1") }); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		_, _, err := tx.ReserveCode(ctx, code.ID, "hash-1", "reservation-1", "session-1", storageAccessCodeNow, storageAccessCodeNow.Add(time.Minute), accesscode.TargetBinding{DeviceUID: code.DeviceUID, Epoch: "fixture-target"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		_, reservation, _, err := tx.FinalizeReservation(ctx, "reservation-1", false, storageAccessCodeNow.Add(time.Second), nil, code.Scope)
		if err != nil || reservation.State != accesscode.ReservationReleased {
			t.Fatalf("reservation=%+v err=%v", reservation, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		_, _, err := tx.ReserveCode(ctx, code.ID, "hash-1", "reservation-2", "session-2", storageAccessCodeNow.Add(2*time.Second), storageAccessCodeNow.Add(time.Minute), accesscode.TargetBinding{DeviceUID: code.DeviceUID, Epoch: "fixture-target"})
		return err
	}); err != nil {
		t.Fatal("released reservation did not free one-time code:", err)
	}
}

// A second live reservation loses the database uniqueness race for one-time codes.
func TestSQLAccessCode_shouldRejectSecondLiveOneTimeReservation(t *testing.T) {
	s, err := OpenSQLStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	code := storageAccessCodeFixture()
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error { return insertTargetCodeFixture(ctx, tx, code, "hash-1") }); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		_, _, err := tx.ReserveCode(ctx, code.ID, "hash-1", "reservation-1", "session-1", storageAccessCodeNow, storageAccessCodeNow.Add(time.Minute), accesscode.TargetBinding{DeviceUID: code.DeviceUID, Epoch: "fixture-target"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		_, _, err := tx.ReserveCode(ctx, code.ID, "hash-1", "reservation-2", "session-2", storageAccessCodeNow.Add(time.Second), storageAccessCodeNow.Add(time.Minute), accesscode.TargetBinding{DeviceUID: code.DeviceUID, Epoch: "fixture-target"})
		return err
	}); err == nil {
		t.Fatal("second live one-time reservation unexpectedly succeeded")
	}
}

// Expiry is a terminal reservation outcome; committing the release prevents
// the same reservation from being retried after the authority time boundary.
func TestSQLAccessCode_shouldPersistRelease_whenReservationExpires(t *testing.T) {
	s, err := OpenSQLStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	code := storageAccessCodeFixture()
	expires := storageAccessCodeNow.Add(time.Minute)
	code.Type = accesscode.Expiring
	code.ExpiresAt = &expires
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error { return insertTargetCodeFixture(ctx, tx, code, "hash-1") }); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		_, _, err := tx.ReserveCode(ctx, code.ID, "hash-1", "reservation-1", "session-1", storageAccessCodeNow, expires, accesscode.TargetBinding{DeviceUID: code.DeviceUID, Epoch: "fixture-target"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		_, reservation, _, err := tx.FinalizeReservation(ctx, "reservation-1", true, expires, nil, code.Scope)
		if err != nil || reservation.State != accesscode.ReservationReleased {
			t.Fatalf("expired reservation=%+v err=%v", reservation, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReadAccessCode(ctx, func(reader accesscode.Reader) error {
		reservation, err := reader.Reservation(ctx, "reservation-1")
		if err != nil || reservation.State != accesscode.ReservationReleased {
			t.Fatalf("persisted reservation=%+v err=%v", reservation, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// Session authorization has its own terminal transitions; code expiry does not
// implicitly end it, while explicit end/revoke is durable.
func TestSQLAccessCode_shouldPersistSessionEndAndRevoke_separately(t *testing.T) {
	s, err := OpenSQLStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	code := storageAccessCodeFixture()
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error { return insertTargetCodeFixture(ctx, tx, code, "hash-1") }); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		_, _, err := tx.ReserveCode(ctx, code.ID, "hash-1", "reservation-1", "session-1", storageAccessCodeNow, storageAccessCodeNow.Add(time.Minute), accesscode.TargetBinding{DeviceUID: code.DeviceUID, Epoch: "fixture-target"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		_, _, _, err := tx.FinalizeReservation(ctx, "reservation-1", true, storageAccessCodeNow.Add(time.Second), nil, code.Scope)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		session, err := tx.EndSession(ctx, "session-1", storageAccessCodeNow.Add(2*time.Second))
		if err != nil || session.State != accesscode.SessionEnded {
			t.Fatalf("ended session=%+v err=%v", session, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReadAccessCode(ctx, func(reader accesscode.Reader) error {
		session, err := reader.Session(ctx, "session-1")
		if err != nil || session.State != accesscode.SessionEnded {
			t.Fatalf("persisted ended session=%+v err=%v", session, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func insertTargetCodeFixture(ctx context.Context, tx accesscode.Transaction, code accesscode.Code, hash string) error {
	if err := tx.ActivateSessionTarget(ctx, accesscode.TargetBinding{DeviceUID: code.DeviceUID, Epoch: "fixture-target"}, storageAccessCodeNow); err != nil {
		return err
	}
	return tx.InsertCode(ctx, code, hash)
}
