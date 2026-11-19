package relay

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/accesscode"
	"github.com/xallor-ai/xallor-remote/internal/account"
	"github.com/xallor-ai/xallor-remote/internal/admission"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// An already authenticated connection carries a stale identity snapshot after
// account/share changes. A valid resume secret cannot bypass current authority.
func TestSessionResumeAuthority_shouldRevalidateBeforeConsumption(t *testing.T) {
	for _, change := range []string{"credential-revoked", "credential-expired", "member-disabled", "member-restored", "share-removed", "share-replaced", "share-expired", "inbound-off", "credential-race", "credential-expiry-race", "share-expiry-race"} {
		t.Run(change, func(t *testing.T) {
			s := openIdentityTestStore(t)
			ctx := context.Background()
			owner := enrollment(t, s, "authority-owner", "")
			guest := enrollment(t, s, "authority-guest", owner.TenantID)
			h := NewHub(s, s, access.NewService(s), nil, admission.New(admission.Limits{PerDevice: protocol.MaxDeviceRequests}), s.SQLStore)
			device, client := &recSender{}, &recSender{}
			if code := h.RegisterDevice("device", "secret", true, device); code != "" {
				t.Fatal(code)
			}
			h.Handle(device, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeDeviceSettings, RequestID: "settings", GrantHash: protocol.SHA256Hex("grant"), Inbound: protocol.Bool(true)})
			controlAccess(t, h, device, owner.Token, access.Request{Action: "claim"})
			state := controlAccess(t, h, device, owner.Token, access.Request{Action: "share", MembershipID: guest.MembershipID, Operations: []string{protocol.OpInfo}, TTLSeconds: 3600})
			authorityExpiry := time.Now().Unix() + 3
			if change == "credential-expiry-race" {
				if _, err := s.faults.Exec(`UPDATE principal_credentials SET expires_at=? WHERE id=?`, authorityExpiry, guest.CredentialID); err != nil {
					t.Fatal(err)
				}
			}
			if change == "share-expiry-race" {
				if _, err := s.faults.Exec(`UPDATE device_shares SET expires_at=? WHERE id=?`, authorityExpiry, state.Shares[0].ID); err != nil {
					t.Fatal(err)
				}
			}
			if code := h.RegisterClient("device", "", guest.Token, client); code != "" {
				t.Fatal(code)
			}
			now := time.Now().UTC()
			code := accesscode.Code{ID: "authority-code", DeviceUID: state.DeviceUID, OwnershipID: state.OwnershipID, Type: accesscode.OneTime, Generation: 1, IssuedAt: now, Scope: []string{protocol.OpInfo}, RequireApproval: true, State: accesscode.StateActive}
			if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
				return tx.InsertCode(ctx, code, protocol.SHA256Hex("session-secret"))
			}); err != nil {
				t.Fatal(err)
			}
			expires := now.Add(time.Hour).Unix()
			h.Handle(client, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeSessionRequest, RequestID: "authority-request", SessionID: "authority-session", TargetUID: state.DeviceUID, CodeID: code.ID, CodeGeneration: 1, SessionScope: code.Scope, SessionExpiresAt: &expires, Grant: "session-secret"})
			approval := device.last()
			h.Handle(device, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeSessionApprovalResult, RequestID: approval.RequestID, SessionID: approval.SessionID, RequestSHA256: approval.RequestSHA256, Allow: protocol.Bool(true)})
			active := client.last()
			if active.Status != protocol.SessionActive || active.ResumeCapability == "" {
				t.Fatal("fixture did not activate")
			}
			h.Drop(client, protocol.RoleClient, "device")
			fresh := &recSender{}
			if code := h.RegisterClient("device", "", guest.Token, fresh); code != "" {
				t.Fatal(code)
			}
			var err error
			switch change {
			case "credential-revoked":
				err = account.New(s).RevokeCredential(ctx, guest.CredentialID)
			case "credential-expired":
				_, err = s.faults.Exec(`UPDATE principal_credentials SET expires_at=? WHERE id=?`, now.Add(-time.Minute).Unix(), guest.CredentialID)
			case "member-disabled", "member-restored":
				err = account.New(s).SetMembershipActive(ctx, guest.MembershipID, false)
				if err == nil && change == "member-restored" {
					err = account.New(s).SetMembershipActive(ctx, guest.MembershipID, true)
				}
			case "share-removed", "share-replaced":
				controlAccess(t, h, device, owner.Token, access.Request{Action: "unshare", ShareID: state.Shares[0].ID})
				if change == "share-replaced" {
					controlAccess(t, h, device, owner.Token, access.Request{Action: "share", MembershipID: guest.MembershipID, Operations: []string{protocol.OpInfo}, TTLSeconds: 3600})
				}
			case "share-expired":
				_, err = s.faults.Exec(`UPDATE device_shares SET expires_at=? WHERE id=?`, now.Add(-time.Minute).Unix(), state.Shares[0].ID)
			case "inbound-off":
				_, err = s.faults.Exec(`UPDATE devices SET inbound=0 WHERE uid=?`, state.DeviceUID)
			}
			if err != nil {
				t.Fatal(err)
			}
			request := protocol.Message{Type: protocol.TypeSessionResumeRequest, RequestID: "stale-resume", SessionID: active.SessionID, ResumeID: active.ResumeID, ResumeCapability: active.ResumeCapability, TargetUID: state.DeviceUID}
			if strings.HasSuffix(change, "-race") {
				barrier := &resumeAuthorityBarrierStore{Store: s.SQLStore, entered: make(chan struct{}), release: make(chan struct{})}
				h.accessCodes = barrier
				done := make(chan struct{})
				var released sync.Once
				unpause := func() { released.Do(func() { close(barrier.release) }) }
				go func() { defer close(done); h.Handle(fresh, protocol.RoleClient, "device", request) }()
				defer func() { unpause(); <-done }()
				select {
				case <-barrier.entered:
				case <-time.After(3 * time.Second):
					t.Fatal("authority read never reached barrier")
				}
				// Commit revocation through a second SQLite connection after the
				// resume transaction read. A stale snapshot cannot consume proof.
				if change == "credential-race" {
					if _, err := s.faults.Exec(`UPDATE principal_credentials SET revoked=1 WHERE id=?`, guest.CredentialID); err != nil {
						t.Fatal(err)
					}
				} else {
					if time.Now().Unix() >= authorityExpiry {
						t.Fatal("authority expired before barrier")
					}
					deadline := time.Now().Add(5 * time.Second)
					for time.Now().Unix() < authorityExpiry {
						if time.Now().After(deadline) {
							t.Fatal("wall-clock expiry never reached")
						}
						time.Sleep(10 * time.Millisecond)
					}
				}
				unpause()
				select {
				case <-done:
				case <-time.After(3 * time.Second):
					t.Fatal("resume did not settle after concurrent revoke")
				}
			} else {
				h.Handle(fresh, protocol.RoleClient, "device", request)
			}
			result := fresh.last()
			if result.Status != protocol.SessionRejectedState || result.Code != protocol.Unauthorized {
				t.Fatalf("stale authority resumed: status=%s code=%s", result.Status, result.Code)
			}
			if err := s.ReadAccessCode(ctx, func(r accesscode.Reader) error {
				proof, err := r.Resume(ctx, active.ResumeID)
				if err != nil {
					return err
				}
				if proof.State != accesscode.ResumeActive {
					return fmt.Errorf("denied request consumed capability")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

type resumeAuthorityBarrierStore struct {
	accesscode.Store
	entered, release chan struct{}
	once             sync.Once
}

func (s *resumeAuthorityBarrierStore) WriteAccessCode(ctx context.Context, fn func(accesscode.Transaction) error) error {
	return s.Store.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		return fn(resumeAuthorityBarrierTx{Transaction: tx, barrier: s})
	})
}

type resumeAuthorityBarrierTx struct {
	accesscode.Transaction
	barrier *resumeAuthorityBarrierStore
}

func (t resumeAuthorityBarrierTx) ResumeAuthority(ctx context.Context, target, credential string) (accesscode.ResumeBinding, error) {
	facts, err := t.Transaction.ResumeAuthority(ctx, target, credential)
	if err != nil {
		return facts, err
	}
	t.barrier.once.Do(func() {
		close(t.barrier.entered)
		select {
		case <-t.barrier.release:
		case <-ctx.Done():
		}
	})
	return facts, ctx.Err()
}
