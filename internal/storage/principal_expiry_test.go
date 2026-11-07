package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/account"
	"github.com/xallor-ai/xallor-remote/internal/principal"
)

// A pool wait crosses the credential deadline. Both a new handshake and an
// existing connection must be rejected using the time after the read completes.
func TestPrincipal_should_rejectExpiredCredential_whenPoolWaitCrossesDeadline(t *testing.T) {
	for _, operation := range []string{"authenticate", "verify"} {
		t.Run(operation, func(t *testing.T) {
			s := openIdentityTestStore(t)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			a, err := account.New(s).CreatePrincipal(ctx, account.CreatePrincipalRequest{Name: "expiry", Kind: "user", TTL: time.Hour})
			if err != nil {
				t.Fatal(err)
			}
			a.ExpiresAt = time.Now().Unix() + 2
			if _, err := s.db.ExecContext(ctx, `UPDATE principal_credentials SET expires_at=? WHERE id=?`, a.ExpiresAt, a.CredentialID); err != nil {
				t.Fatal(err)
			}
			held, err := s.db.Conn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer held.Close()
			before := s.db.Stats().WaitCount
			done := make(chan error, 1)
			go func() {
				if operation == "authenticate" {
					_, err := principal.NewService(s).Authenticate(ctx, a.Token)
					done <- err
				} else {
					done <- principal.NewService(s).Verify(ctx, a.Identity)
				}
			}()
			for s.db.Stats().WaitCount == before {
				if ctx.Err() != nil {
					t.Fatal("credential read did not reach pool wait")
				}
				time.Sleep(time.Millisecond)
			}
			if time.Now().Unix() >= a.ExpiresAt {
				t.Fatal("read did not start before expiry")
			}
			time.Sleep(time.Until(time.Unix(a.ExpiresAt, 0)) + 20*time.Millisecond)
			_ = held.Close()
			if err := <-done; !errors.Is(err, principal.ErrUnauthorized) {
				t.Fatalf("expired credential was accepted after pool wait: %v", err)
			}
		})
	}
}
