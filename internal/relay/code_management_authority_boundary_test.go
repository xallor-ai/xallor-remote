package relay

import (
	"context"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/accesscode"
	"github.com/xallor-ai/xallor-remote/internal/account"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Owner management is independent of inbound session admission: closing the
// device to new work must not prevent the owner revoking/rotating its codes.
func TestCodeManagementAuthority_shouldAllowValidOwner_whenInboundIsDisabled(t *testing.T) {
	for _, action := range []string{"code.revoke", "code.rotate"} {
		t.Run(action, func(t *testing.T) {
			_, h, target, _, _, token := typedCodeFixture(t)
			h.Handle(target, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeDeviceSettings, RequestID: "off", GrantHash: protocol.SHA256Hex("changed-unused-owned-grant"), Inbound: protocol.Bool(false)})
			if target.last().Code != "" || h.devices["device"].record.Inbound {
				t.Fatal("inbound-off precondition failed")
			}
			got := sendTypedAccess(t, h, target, token, managementAuthorityRequest(action))
			if got.Code != "" || got.DeviceContext == nil || got.DeviceContext.Inbound {
				t.Fatal("valid owner blocked or inactive context changed", got.Code)
			}
		})
	}
}

// Finding a matching rotation receipt is not a credential: each new request
// must revalidate the actor even when it would otherwise be an idempotent read.
func TestCodeManagementAuthority_shouldDenyReceiptRetry_afterMembershipChanges(t *testing.T) {
	s, h, target, _, _, token := typedCodeFixture(t)
	ctx := context.Background()
	actor, err := s.Authenticate(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	req := managementAuthorityRequest("code.rotate")
	if got := sendTypedAccess(t, h, target, token, req); got.Code != "" {
		t.Fatal("initial rotation failed", got.Code)
	}
	changed := false
	h.accessCodes = &beforeCodeManagementStore{Store: s.SQLStore, before: func() {
		changed = true
		if err := account.New(s).SetMembershipActive(ctx, actor.MembershipID, false); err != nil {
			t.Fatal(err)
		}
		if err := account.New(s).SetMembershipActive(ctx, actor.MembershipID, true); err != nil {
			t.Fatal(err)
		}
	}}
	got := sendTypedAccess(t, h, target, token, req)
	if !changed || got.Code != protocol.Unauthorized || len(got.Payload) != 0 {
		t.Fatal("receipt bypassed current authority", got.Code)
	}
	if err := s.ReadAccessCode(ctx, func(r accesscode.Reader) error {
		child, err := r.Code(ctx, req.NewCodeID)
		if err != nil {
			return err
		}
		if child.Generation != 2 || child.State != accesscode.StateActive {
			t.Error("denied retry changed replacement")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

type afterCommittedCodeStore struct {
	accesscode.Store
	after func() error
}

func (s afterCommittedCodeStore) WriteAccessCode(ctx context.Context, fn func(accesscode.Transaction) error) error {
	if err := s.Store.WriteAccessCode(ctx, fn); err != nil {
		return err
	}
	return s.after()
}

// A later credential revocation does not retroactively undo a management
// mutation that already passed transactional authorization and committed.
func TestCodeManagementAuthority_shouldPreserveCommittedResult_whenCredentialRevocationComesLater(t *testing.T) {
	for _, action := range []string{"code.revoke", "code.rotate"} {
		t.Run(action, func(t *testing.T) {
			s, h, target, _, _, token := typedCodeFixture(t)
			ctx := context.Background()
			actor, err := s.Authenticate(ctx, token)
			if err != nil {
				t.Fatal(err)
			}
			changed := false
			h.accessCodes = afterCommittedCodeStore{Store: s.SQLStore, after: func() error { changed = true; return account.New(s).RevokeCredential(ctx, actor.CredentialID) }}
			got := sendTypedAccess(t, h, target, token, managementAuthorityRequest(action))
			if !changed || got.Code != "" {
				t.Fatal("later revocation hid already-committed management", got.Code)
			}
			if _, err := s.Authenticate(ctx, token); err == nil {
				t.Fatal("later credential revocation did not commit")
			}
		})
	}
}

// A missing table is a real SQL failure, not a missing-row permission denial.
// The temporary database is owned by this test and never shared with a service.
func TestCodeManagementAuthority_shouldKeepSQLReadFailureUnknown(t *testing.T) {
	s, h, target, _, _, token := typedCodeFixture(t)
	h.accessCodes = &beforeCodeManagementStore{Store: s.SQLStore, before: func() {
		if _, err := s.faults.Exec(`DROP TABLE principal_credentials`); err != nil {
			t.Fatal(err)
		}
	}}
	got := sendTypedAccess(t, h, target, token, managementAuthorityRequest("code.revoke"))
	if got.Code != protocol.RelayError || h.DeviceOnline("device") {
		t.Fatal("SQL fault treated as permission denial or success", got.Code)
	}
	assertManagedCodeActive(t, s)
}
