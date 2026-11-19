package storage

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/accesscode"
)

// Two real DB connections may race the same or different intents. A parent
// gets only one replacement; siblings allocate unique device generations.
func TestCodeRotation_shouldSerializeConcurrentIntents_acrossConnections(t *testing.T) {
	for _, kind := range []string{"same-intent", "conflicting-intent", "siblings"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			s := codeManagementFixtureAt(t, dir)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			old2, new2, hash2 := "resume-code", "replacement", strings.Repeat("b", 64)
			if kind == "conflicting-intent" {
				new2 = "different"
			}
			if kind == "siblings" {
				old2, new2, hash2 = "sibling", "sibling-new", strings.Repeat("c", 64)
				if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
					return tx.InsertCode(ctx, accesscode.Code{ID: old2, DeviceUID: "device", Generation: 2, Type: accesscode.LongLived, IssuedAt: resumeStorageNow, Scope: []string{"info"}, State: accesscode.StateActive}, "sibling-hash")
				}); err != nil {
					t.Fatal(err)
				}
			}
			other, err := OpenSQLStore(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer other.Close()
			type outcome struct {
				code accesscode.Code
				err  error
			}
			results := make(chan outcome, 2)
			start := make(chan struct{})
			var wg sync.WaitGroup
			for _, op := range []struct {
				s             *SQLStore
				old, id, hash string
			}{{s, "resume-code", "replacement", strings.Repeat("b", 64)}, {other, old2, new2, hash2}} {
				op := op
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					var result accesscode.Code
					err := op.s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
						var err error
						result, err = tx.RotateCode(ctx, op.old, op.id, op.hash, resumeStorageNow)
						return err
					})
					results <- outcome{result, err}
				}()
			}
			close(start)
			wg.Wait()
			close(results)
			success := []accesscode.Code{}
			failures := 0
			for result := range results {
				if result.err == nil {
					success = append(success, result.code)
				} else {
					failures++
					if !errors.Is(result.err, accesscode.ErrRotationConflict) {
						t.Error(result.err)
					}
				}
			}
			if kind == "conflicting-intent" {
				if len(success) != 1 || failures != 1 {
					t.Fatal("parent rotated more than once")
				}
			} else if len(success) != 2 {
				t.Fatal("valid concurrent intent failed")
			}
			if kind == "same-intent" && success[0].ID != success[1].ID {
				t.Error("retry minted another code")
			}
			if kind == "siblings" && (success[0].Generation == success[1].Generation || success[0].Generation < 3 || success[1].Generation < 3) {
				t.Error("device generation allocation collided")
			}
		})
	}
}

// A finalized session or consumed resume based on a stale read must lose to a
// revocation/rotation committed through a separate SQLite connection.
func TestCodeManagement_shouldFenceStaleSnapshot_whenRevocationCommitsFirst(t *testing.T) {
	for _, change := range []string{"revoke", "rotate"} {
		for _, operation := range []string{"finalize", "resume"} {
			t.Run(change+"/"+operation, func(t *testing.T) {
				dir := t.TempDir()
				s := codeManagementFixtureAt(t, dir)
				other, err := OpenSQLStore(dir)
				if err != nil {
					t.Fatal(err)
				}
				defer other.Close()
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				entered, release := make(chan struct{}), make(chan struct{})
				var once, releaseOnce sync.Once
				unpause := func() { releaseOnce.Do(func() { close(release) }) }
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
						if operation == "finalize" {
							_, _, _, err := tx.FinalizeReservation(ctx, "pending", true, resumeStorageNow, nil, []string{"info"})
							return err
						}
						proof := resumeStorageFixture()
						_, err := tx.ConsumeResume(ctx, proof.ID, proof.CapabilityHash, resumeStorageNow.Add(time.Second), proof.SessionID, proof.BindingKind, proof.BindingDigest)
						return err
					})
				}()
				select {
				case <-entered:
				case <-ctx.Done():
					unpause()
					<-done
					t.Fatal("read barrier not reached")
				}
				err = other.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
					if change == "revoke" {
						_, err := tx.RevokeCode(ctx, "resume-code")
						return err
					}
					_, err := tx.RotateCode(ctx, "resume-code", "replacement", strings.Repeat("b", 64), resumeStorageNow)
					return err
				})
				unpause()
				staleErr := <-done
				if err != nil {
					t.Fatal(err)
				}
				want := accesscode.ErrSessionState
				if operation == "finalize" {
					want = accesscode.ErrReservationState
				}
				if !errors.Is(staleErr, want) {
					t.Fatalf("stale operation outcome=%v want=%v", staleErr, want)
				}
				if err := s.ReadAccessCode(ctx, func(r accesscode.Reader) error {
					ss, err := r.Session(ctx, "resume-session")
					if ss.State != accesscode.SessionRevoked {
						t.Error("old session survived")
					}
					return err
				}); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
