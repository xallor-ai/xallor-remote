package relay

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/accesscode"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

type afterCodeAuthorityStore struct {
	accesscode.Store
	once      sync.Once
	reads     int
	after     func(context.Context) error
	changeErr error
}

func (s *afterCodeAuthorityStore) WriteAccessCode(ctx context.Context, fn func(accesscode.Transaction) error) error {
	return s.Store.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		return fn(afterCodeAuthorityTransaction{Transaction: tx, store: s})
	})
}

type afterCodeAuthorityTransaction struct {
	accesscode.Transaction
	store *afterCodeAuthorityStore
}

func (t afterCodeAuthorityTransaction) CodeManagementAuthority(ctx context.Context, target, credential string) (accesscode.ManagementAuthority, error) {
	t.store.reads++
	value, err := t.Transaction.CodeManagementAuthority(ctx, target, credential)
	if err != nil {
		return value, err
	}
	t.store.once.Do(func() { t.store.changeErr = t.store.after(ctx) })
	if t.store.changeErr != nil {
		return value, t.store.changeErr
	}
	return value, nil
}

// A second real SQLite connection commits AFTER the first authority snapshot.
// Its write makes the original snapshot unwriteable; confirmed rollback/retry
// must re-read the changed authority instead of replaying an old authorization.
func TestCodeManagementAuthority_shouldRecheckAfterSQLiteSnapshotConflict(t *testing.T) {
	for _, action := range []string{"code.revoke", "code.rotate"} {
		for _, change := range []string{"credential", "member-version", "device-secret"} {
			t.Run(action+"/"+change, func(t *testing.T) {
				s, h, target, _, _, token := typedCodeFixture(t)
				actor, err := s.Authenticate(context.Background(), token)
				if err != nil {
					t.Fatal(err)
				}
				window := &afterCodeAuthorityStore{Store: s.SQLStore, after: func(ctx context.Context) error {
					var query string
					var args []any
					switch change {
					case "credential":
						query, args = `UPDATE principal_credentials SET revoked=1 WHERE id=?`, []any{actor.CredentialID}
					case "member-version":
						query, args = `UPDATE memberships SET version=version+1 WHERE id=?`, []any{actor.MembershipID}
					case "device-secret":
						query, args = `UPDATE devices SET secret_hash=? WHERE id=?`, []any{protocol.SHA256Hex("changed-secret"), "device"}
					}
					result, err := s.faults.ExecContext(ctx, query, args...)
					if err != nil {
						return err
					}
					if count, err := result.RowsAffected(); err != nil || count != 1 {
						return errors.New("independent authority write did not commit one row")
					}
					return nil
				}}
				h.accessCodes = window
				req := managementAuthorityRequest(action)
				got := sendTypedAccess(t, h, target, token, req)
				if window.changeErr != nil || window.reads != 2 {
					t.Fatalf("real snapshot retry not exercised: reads=%d change=%v", window.reads, window.changeErr)
				}
				if got.Code != protocol.Unauthorized {
					t.Fatal("stale authority survived retry", got.Code)
				}
				assertManagedCodeActive(t, s)
				t.Logf("independent SQLite write committed after snapshot; authority reads=%d; mutation denied", window.reads)
			})
		}
	}
}

// Time is sampled after the authority read returns, not before a pool/read wait.
func TestCodeManagementAuthority_shouldRejectExpiry_duringAuthorityRead(t *testing.T) {
	for _, action := range []string{"code.revoke", "code.rotate"} {
		t.Run(action, func(t *testing.T) {
			s, h, target, _, _, token := typedCodeFixture(t)
			actor, err := s.Authenticate(context.Background(), token)
			if err != nil {
				t.Fatal(err)
			}
			expires := time.Now().Add(2 * time.Second).Unix()
			if _, err := s.faults.Exec(`UPDATE principal_credentials SET expires_at=? WHERE id=?`, expires, actor.CredentialID); err != nil {
				t.Fatal(err)
			}
			window := &afterCodeAuthorityStore{Store: s.SQLStore, after: func(ctx context.Context) error {
				timer := time.NewTimer(time.Until(time.Unix(expires, 0)))
				defer timer.Stop()
				select {
				case <-timer.C:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}}
			h.accessCodes = window
			got := sendTypedAccess(t, h, target, token, managementAuthorityRequest(action))
			if window.reads != 1 || window.changeErr != nil || got.Code != protocol.Unauthorized {
				t.Fatal("expired authority accepted or wrong barrier", window.reads, got.Code, window.changeErr)
			}
			assertManagedCodeActive(t, s)
		})
	}
}

// Infrastructure failure is not a proven permission denial; quarantine the
// routing cache and preserve the management outcome as unknown without replay.
func TestCodeManagementAuthority_shouldQuarantine_whenAuthorityReadFails(t *testing.T) {
	s, h, target, _, _, token := typedCodeFixture(t)
	window := &afterCodeAuthorityStore{Store: s.SQLStore, after: func(context.Context) error { return errors.New("injected authority read failure") }}
	h.accessCodes = window
	got := sendTypedAccess(t, h, target, token, managementAuthorityRequest("code.rotate"))
	if got.Code != protocol.RelayError || h.DeviceOnline("device") || window.reads != 1 {
		t.Fatal("storage failure mislabeled/replayed", got.Code, window.reads)
	}
	assertManagedCodeActive(t, s)
}

func managementAuthorityRequest(action string) access.Request {
	req := access.Request{Action: action, CodeID: "managed-code"}
	if action == "code.rotate" {
		req.NewCodeID, req.NewSecret = "new-managed-code", "new-managed-secret"
	}
	return req
}

func assertManagedCodeActive(t *testing.T, s *relaySQLFixture) {
	t.Helper()
	ctx := context.Background()
	var code accesscode.Code
	var receiptErr error
	if err := s.ReadAccessCode(ctx, func(r accesscode.Reader) error {
		var err error
		code, err = r.Code(ctx, "managed-code")
		if err != nil {
			return err
		}
		_, receiptErr = r.Rotation(ctx, "managed-code")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if code.State != accesscode.StateActive || !errors.Is(receiptErr, accesscode.ErrRotationNotFound) {
		t.Fatal("denied operation mutated code or receipt")
	}
}
