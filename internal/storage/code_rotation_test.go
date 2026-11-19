package storage

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/accesscode"
)

func unusedRotationFixture(t *testing.T, kind accesscode.Type) (*SQLStore, accesscode.Code, accesscode.TargetBinding) {
	t.Helper()
	s, err := OpenSQLStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	target := accesscode.TargetBinding{DeviceUID: "device", Epoch: "epoch"}
	code := accesscode.Code{ID: "unused", DeviceUID: "device", Generation: 1, Type: kind, IssuedAt: resumeStorageNow, Scope: []string{"info"}, RequireApproval: true, State: accesscode.StateActive}
	if kind == accesscode.Expiring {
		expires := resumeStorageNow.Add(time.Minute)
		code.ExpiresAt = &expires
	}
	ctx := context.Background()
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		if err := tx.ActivateSessionTarget(ctx, target, resumeStorageNow); err != nil {
			return err
		}
		return tx.InsertCode(ctx, code, strings.Repeat("a", 64))
	}); err != nil {
		t.Fatal(err)
	}
	return s, code, target
}

// Rotating an encumbered one-time credential must not allocate a fresh use,
// even if legacy metadata says active or multiple unknown histories exist.
func TestCodeRotation_shouldPreserveOneTimeEligibility_whenHistoryExists(t *testing.T) {
	for _, state := range []string{"unused", "reserved", "unknown", "multiple-unknown", "consumed", "consumed-metadata-active", "released"} {
		t.Run(state, func(t *testing.T) {
			s, code, target := unusedRotationFixture(t, accesscode.OneTime)
			ctx := context.Background()
			if state != "unused" {
				if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
					if _, _, err := tx.ReserveCode(ctx, code.ID, strings.Repeat("a", 64), "attempt", "session", resumeStorageNow, resumeStorageNow.Add(time.Minute), target); err != nil {
						return err
					}
					switch state {
					case "unknown", "multiple-unknown":
						_, err := tx.MarkReservationUnknown(ctx, "attempt", resumeStorageNow)
						return err
					case "consumed", "consumed-metadata-active", "released":
						_, _, _, err := tx.FinalizeReservation(ctx, "attempt", state != "released", resumeStorageNow, nil, code.Scope)
						return err
					default:
						return nil
					}
				}); err != nil {
					t.Fatal(err)
				}
			}
			if state == "consumed-metadata-active" {
				if _, err := s.db.Exec(`UPDATE access_codes SET state='active' WHERE code_id='unused'`); err != nil {
					t.Fatal(err)
				}
			}
			if state == "multiple-unknown" {
				if _, err := s.db.Exec(`INSERT INTO code_consumptions(reservation_id,code_id,generation,session_id,state,exclusive,reserved_at,reservation_expires_at,target_uid,target_epoch) SELECT 'second',code_id,generation,'second-session','unknown',exclusive,reserved_at,reservation_expires_at,target_uid,target_epoch FROM code_consumptions WHERE reservation_id='attempt'`); err != nil {
					t.Fatal(err)
				}
			}
			err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
				_, err := tx.RotateCode(ctx, code.ID, "replacement", strings.Repeat("b", 64), resumeStorageNow.Add(time.Second))
				return err
			})
			allowed := state == "unused" || state == "released"
			if (err == nil) != allowed {
				t.Fatalf("rotation allowed=%v for %s: %v", err == nil, state, err)
			}
			if !allowed {
				var count int
				if err := s.db.QueryRow(`SELECT count(*) FROM access_codes WHERE code_id='replacement'`).Scan(&count); err != nil || count != 0 {
					t.Fatal("refused rotation minted eligibility", err)
				}
			}
		})
	}
}

// Rotation preserves the original deadline. Repeated intent after that deadline
// reads the receipt, while a first attempt at/after expiry cannot extend access.
func TestCodeRotation_shouldKeepDeadline_andNotRenewOnRetry(t *testing.T) {
	for _, offset := range []time.Duration{-time.Second, 0, time.Second} {
		t.Run(offset.String(), func(t *testing.T) {
			s, code, _ := unusedRotationFixture(t, accesscode.Expiring)
			ctx := context.Background()
			at := code.ExpiresAt.Add(offset)
			var next accesscode.Code
			err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
				var err error
				next, err = tx.RotateCode(ctx, code.ID, "replacement", strings.Repeat("b", 64), at)
				return err
			})
			if offset >= 0 {
				if !errors.Is(err, accesscode.ErrCodeExpired) {
					t.Fatal("expired code rotated", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if next.ExpiresAt == nil || !next.ExpiresAt.Equal(*code.ExpiresAt) || next.RequireApproval != code.RequireApproval || next.Type != code.Type {
				t.Fatal("rotation changed authorization policy")
			}
			if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
				again, err := tx.RotateCode(ctx, code.ID, "replacement", strings.Repeat("b", 64), at.Add(time.Hour))
				if err == nil && (!again.IssuedAt.Equal(next.IssuedAt) || !again.ExpiresAt.Equal(*code.ExpiresAt)) {
					t.Error("retry extended deadline")
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// Receipt reuse cannot reactivate a replacement after it was consumed or itself
// rotated; observing the original result never follows a later credential link.
func TestCodeRotation_shouldObserveCurrentReplacement_withoutReissuing(t *testing.T) {
	for _, state := range []string{"consumed", "rotated"} {
		t.Run(state, func(t *testing.T) {
			s, code, target := unusedRotationFixture(t, accesscode.OneTime)
			ctx := context.Background()
			hash := strings.Repeat("b", 64)
			if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
				_, err := tx.RotateCode(ctx, code.ID, "replacement", hash, resumeStorageNow)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			want := accesscode.StateConsumed
			if state == "consumed" {
				if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
					if _, _, err := tx.ReserveCode(ctx, "replacement", hash, "new-use", "new-session", resumeStorageNow, resumeStorageNow.Add(time.Minute), target); err != nil {
						return err
					}
					_, _, _, err := tx.FinalizeReservation(ctx, "new-use", true, resumeStorageNow, nil, code.Scope)
					return err
				}); err != nil {
					t.Fatal(err)
				}
			} else {
				want = accesscode.StateRevoked
				if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
					_, err := tx.RotateCode(ctx, "replacement", "third", strings.Repeat("c", 64), resumeStorageNow)
					return err
				}); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
				got, err := tx.RotateCode(ctx, code.ID, "replacement", hash, resumeStorageNow.Add(time.Hour))
				if err == nil && (got.ID != "replacement" || got.State != want) {
					t.Error("receipt followed/revived a later credential")
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// Invalid/same-secret intent and SQLite generation exhaustion must leave the
// original code and its eligibility unchanged.
func TestCodeRotation_shouldRejectUnsafeIntent_beforeRevokingSource(t *testing.T) {
	for _, kind := range []string{"same-secret", "same-id", "bad-hash", "uppercase-hash", "generation-overflow"} {
		t.Run(kind, func(t *testing.T) {
			s, code, _ := unusedRotationFixture(t, accesscode.LongLived)
			ctx := context.Background()
			id, hash := "replacement", strings.Repeat("b", 64)
			switch kind {
			case "same-secret":
				hash = strings.Repeat("a", 64)
			case "same-id":
				id = code.ID
			case "bad-hash":
				hash = "hash"
			case "uppercase-hash":
				hash = strings.Repeat("B", 64)
			case "generation-overflow":
				if _, err := s.db.Exec(`UPDATE access_codes SET generation=9223372036854775807`); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
				_, err := tx.RotateCode(ctx, code.ID, id, hash, resumeStorageNow)
				return err
			}); err == nil {
				t.Fatal("unsafe intent accepted")
			}
			if err := s.ReadAccessCode(ctx, func(r accesscode.Reader) error {
				got, err := r.Code(ctx, code.ID)
				if got.State != accesscode.StateActive {
					t.Error("failed intent revoked source")
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
