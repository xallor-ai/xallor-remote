package relay

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/accesscode"
	"github.com/xallor-ai/xallor-remote/internal/account"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

type beforeCodeManagementStore struct {
	accesscode.Store
	once   sync.Once
	before func()
}

func (s *beforeCodeManagementStore) WriteAccessCode(ctx context.Context, fn func(accesscode.Transaction) error) error {
	s.once.Do(s.before)
	return s.Store.WriteAccessCode(ctx, fn)
}

// The device and principal were valid at Authenticate. Changes committed by an
// independent management path before code mutation must be re-read atomically;
// denied operations must not change code/session/resume/rotation state.
func TestCodeManagementAuthority_shouldRejectStaleActorOrDevice_beforeTransaction(t *testing.T) {
	for _, action := range []string{"code.revoke", "code.rotate"} {
		for _, change := range []string{"credential-revoked", "credential-deleted", "membership-disabled", "membership-reenabled", "credential-expired", "ownership-transferred", "device-deleted", "device-recreated", "device-secret-changed"} {
			t.Run(action+"/"+change, func(t *testing.T) {
				s, h, target, client, device, token := typedCodeFixture(t)
				ctx := context.Background()
				actor, err := s.Authenticate(ctx, token)
				if err != nil {
					t.Fatal(err)
				}
				other := enrollment(t, s, "new-code-owner", "")
				expires := time.Now().Add(time.Hour).Unix()
				approveSession(t, h, target, client, protocol.Message{Type: protocol.TypeSessionRequest, RequestID: "authority-request", SessionID: "authority-session", TargetID: "device", TargetUID: device.UID, CodeID: "managed-code", CodeGeneration: 1, Grant: "managed-secret", SessionScope: []string{protocol.OpInfo}, SessionExpiresAt: &expires})
				active := client.last()
				if active.Status != protocol.SessionActive {
					t.Fatal("initial session not active", active.Code)
				}
				var before string
				called := false
				h.accessCodes = &beforeCodeManagementStore{Store: s.SQLStore, before: func() {
					called = true
					var err error
					switch change {
					case "credential-revoked":
						err = account.New(s).RevokeCredential(ctx, actor.CredentialID)
					case "credential-deleted":
						_, err = s.faults.Exec(`DELETE FROM principal_credentials WHERE id=?`, actor.CredentialID)
					case "membership-disabled":
						err = account.New(s).SetMembershipActive(ctx, actor.MembershipID, false)
					case "membership-reenabled":
						err = account.New(s).SetMembershipActive(ctx, actor.MembershipID, false)
						if err == nil {
							err = account.New(s).SetMembershipActive(ctx, actor.MembershipID, true)
						}
					case "credential-expired":
						_, err = s.faults.Exec(`UPDATE principal_credentials SET expires_at=? WHERE id=?`, time.Now().Add(-time.Second).Unix(), actor.CredentialID)
					case "ownership-transferred":
						_, err = access.NewService(s).Manage(ctx, "device", device, actor, access.Request{Action: "transfer", MembershipID: other.Identity.MembershipID})
					case "device-recreated":
						err = s.DeleteDevice(ctx, "device", device.UID, device.SecretHash)
						if err == nil {
							_, err = s.SQLStore.RegisterDevice(ctx, "device", protocol.SHA256Hex("new-device-secret"), true)
						}
					case "device-deleted":
						err = s.DeleteDevice(ctx, "device", device.UID, device.SecretHash)
					case "device-secret-changed":
						_, err = s.faults.Exec(`UPDATE devices SET secret_hash=? WHERE id=?`, protocol.SHA256Hex("new-device-secret"), "device")
					}
					if err != nil {
						t.Fatal("authority change failed", err)
					}
					before = codeAuthorityState(t, s, active)
				}}
				req := access.Request{Action: action, CodeID: "managed-code"}
				if action == "code.rotate" {
					req.NewCodeID, req.NewSecret = "new-managed-code", "new-managed-secret"
				}
				got := sendTypedAccess(t, h, target, token, req)
				if !called {
					t.Fatal("transaction boundary not reached")
				}
				if got.Code != protocol.Unauthorized {
					t.Errorf("stale authority accepted: code=%s", got.Code)
				}
				if after := codeAuthorityState(t, s, active); before != after {
					t.Error("stale authority mutated code family or receipt")
				}
			})
		}
	}
}

func codeAuthorityState(t *testing.T, s *relaySQLFixture, active protocol.Message) string {
	t.Helper()
	ctx := context.Background()
	var state struct {
		Code        accesscode.Code
		Session     accesscode.SessionAuthorization
		Resume      accesscode.ResumeCapability
		Rotation    accesscode.Rotation
		HasRotation bool
	}
	err := s.ReadAccessCode(ctx, func(r accesscode.Reader) error {
		var err error
		if state.Code, err = r.Code(ctx, "managed-code"); err != nil {
			return err
		}
		if state.Session, err = r.Session(ctx, active.SessionID); err != nil {
			return err
		}
		if state.Resume, err = r.Resume(ctx, active.ResumeID); err != nil {
			return err
		}
		state.Rotation, err = r.Rotation(ctx, "managed-code")
		if errors.Is(err, accesscode.ErrRotationNotFound) {
			return nil
		}
		state.HasRotation = err == nil
		return err
	})
	if err != nil {
		t.Fatal("code state observation failed", err)
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
