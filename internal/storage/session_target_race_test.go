package storage

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/accesscode"
)

// A second real SQLite connection rotates while the first has a read snapshot.
// Retrying a rolled-back busy write must re-read the epoch, not revive consent.
func TestSessionTargetStorage_shouldRejectStaleSnapshot_whenAnotherConnectionRotates(t *testing.T) {
	for _, operation := range []string{"reserve", "finalize", "consume"} {
		t.Run(operation, func(t *testing.T) {
			dir := t.TempDir()
			s, err := OpenSQLStore(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			insertResumeSession(t, s)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			old := accesscode.TargetBinding{DeviceUID: "device", Epoch: "resume-target"}
			proof := resumeStorageFixture()
			if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
				if err := tx.InsertResume(ctx, proof); err != nil {
					return err
				}
				_, _, err := tx.ReserveCode(ctx, "resume-code", "hash", "pending", "pending-session", resumeStorageNow, resumeStorageNow.Add(time.Minute), old)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			other, err := OpenSQLStore(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer other.Close()
			entered, release := make(chan struct{}), make(chan struct{})
			var once, releaseOnce sync.Once
			unpause := func() { releaseOnce.Do(func() { close(release) }) }
			defer unpause()
			done := make(chan error, 1)
			go func() {
				done <- s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
					if _, err := tx.Code(ctx, "resume-code"); err != nil {
						return err
					}
					once.Do(func() {
						close(entered)
						select {
						case <-release:
						case <-ctx.Done():
						}
					})
					switch operation {
					case "reserve":
						_, _, err := tx.ReserveCode(ctx, "resume-code", "hash", "late", "late-session", resumeStorageNow, resumeStorageNow.Add(time.Minute), old)
						return err
					case "finalize":
						_, _, _, err := tx.FinalizeReservation(ctx, "pending", true, resumeStorageNow, nil, []string{"info"})
						return err
					default:
						_, err := tx.ConsumeResume(ctx, proof.ID, proof.CapabilityHash, resumeStorageNow.Add(time.Second), proof.SessionID, proof.BindingKind, proof.BindingDigest)
						return err
					}
				})
			}()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("read barrier not reached")
			}
			rotationErr := other.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
				return tx.ActivateSessionTarget(ctx, accesscode.TargetBinding{DeviceUID: "device", Epoch: "new"}, resumeStorageNow)
			})
			unpause()
			err = <-done
			if rotationErr != nil {
				t.Fatal(rotationErr)
			}
			if !errors.Is(err, accesscode.ErrTargetEpoch) && !errors.Is(err, accesscode.ErrSessionState) {
				t.Fatalf("stale %s outcome=%v", operation, err)
			}
			if err := s.ReadAccessCode(ctx, func(r accesscode.Reader) error {
				p, err := r.Resume(ctx, proof.ID)
				if err != nil {
					return err
				}
				if p.State != accesscode.ResumeRevoked {
					t.Error("stale transaction consumed proof")
				}
				pending, err := r.Reservation(ctx, "pending")
				if err != nil {
					return err
				}
				if pending.State != accesscode.ReservationUnknown {
					t.Error("old pending finalized")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
