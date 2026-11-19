package relay

import (
	"context"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/accesscode"
	"github.com/xallor-ai/xallor-remote/internal/delegation"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

func sessionExecutionFixture(t *testing.T) (*relaySQLFixture, *Hub, *recSender, *settingsController, protocol.Message, string) {
	t.Helper()
	s, h, target, _ := approvedSessionFixture(t)
	h.executionAuthority = delegation.NewAuthority([32]byte{1}, s, nil, nil)
	client := &settingsController{recSender: &recSender{}}
	request := nextSessionRequest(t, s, h, client, "lease", 2, protocol.OpExec)
	expires := time.Now().Add(5 * time.Minute).Unix()
	request.SessionExpiresAt = &expires
	approveSession(t, h, target, client, request)
	if client.last().Status != protocol.SessionActive {
		t.Fatal("session did not activate")
	}
	h.Handle(client, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeInvoke, ExecID: "run", SessionID: request.SessionID, Op: protocol.OpExec})
	route := target.last().ExecID
	if h.inflight[route] == nil {
		t.Fatal("execution did not route")
	}
	return s, h, target, client, request, route
}

// Execution renewal must revalidate durable recipient consent, not only the
// still-valid transport grant. Revocation does not synthesize a task terminal.
func TestSessionExecutionLease_shouldRejectInvalidConsent_withoutReleasingRoute(t *testing.T) {
	for _, change := range []string{"session-end", "session-revoke", "code-revoke", "session-expired", "scope-removed"} {
		t.Run(change, func(t *testing.T) {
			s, h, target, client, request, route := sessionExecutionFixture(t)
			ctx := context.Background()
			var err error
			switch change {
			case "session-end":
				err = s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
					_, err := tx.EndSession(ctx, request.SessionID, time.Now())
					return err
				})
			case "session-revoke":
				err = s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
					_, err := tx.RevokeSession(ctx, request.SessionID, time.Now())
					return err
				})
			case "code-revoke":
				err = s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error { _, err := tx.RevokeCode(ctx, request.CodeID); return err })
			case "session-expired":
				_, err = s.faults.Exec(`UPDATE session_authorizations SET expires_at=? WHERE session_id=?`, time.Now().Add(-time.Second).Unix(), request.SessionID)
			case "scope-removed":
				_, err = s.faults.Exec(`UPDATE session_authorizations SET scope_json='["info"]' WHERE session_id=?`, request.SessionID)
			}
			if err != nil {
				t.Fatal(err)
			}
			h.Handle(target, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeExecutionRenewRoute, RequestID: "renew", ExecID: route})
			if got := target.last(); got.Code != protocol.Unauthorized || got.LeaseMS != 0 {
				t.Errorf("invalid session renewed: code=%s ttl=%d", got.Code, got.LeaseMS)
			}
			uid := h.devices["device"].record.UID
			if client.closed || h.inflight[route] == nil || h.admission.Active(uid) != 1 {
				t.Error("denial closed stream or falsely released capacity")
			}
			h.Handle(target, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeStdout, ExecID: route, Data: "remaining-output"})
			if client.last().Data != "remaining-output" {
				t.Error("remaining output lost")
			}
			h.Handle(target, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeExit, ExecID: route, Status: protocol.ExitAuthorizationLost})
			if client.last().Status != protocol.ExitAuthorizationLost || h.admission.Active(uid) != 0 {
				t.Error("terminal settlement changed")
			}
		})
	}
}

// A renewable execution lease cannot outlive its logical session even though
// the underlying transport/identity authority grants the usual twenty seconds.
func TestSessionExecutionLease_shouldCapTTL_toSessionDeadline(t *testing.T) {
	s, h, target, _, request, route := sessionExecutionFixture(t)
	expires := time.Now().Add(3 * time.Second).Truncate(time.Second)
	if _, err := s.faults.Exec(`UPDATE session_authorizations SET expires_at=? WHERE session_id=?`, expires.Unix(), request.SessionID); err != nil {
		t.Fatal(err)
	}
	h.inflight[route].binding.session.ExpiresAt = &expires
	h.Handle(target, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeExecutionRenewRoute, RequestID: "short", ExecID: route})
	got := target.last()
	if got.Code != "" || got.LeaseMS <= 0 || got.LeaseMS > 3000 {
		t.Fatalf("lease exceeds session deadline: code=%s ttl=%d", got.Code, got.LeaseMS)
	}
}

// Natural access-code expiry is not session revocation; an already approved
// execution follows the session's own expiry and remains renewable.
func TestSessionExecutionLease_shouldPreserveApprovedSession_whenOnlyCodeExpires(t *testing.T) {
	s, h, target, _, request, route := sessionExecutionFixture(t)
	if _, err := s.faults.Exec(`UPDATE access_codes SET type='expiring',issued_at=?,expires_at=?,state='expired' WHERE code_id=?`, time.Now().Add(-time.Minute).Unix(), time.Now().Add(-time.Second).Unix(), request.CodeID); err != nil {
		t.Fatal(err)
	}
	h.Handle(target, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeExecutionRenewRoute, RequestID: "renew", ExecID: route})
	if got := target.last(); got.Code != "" || got.LeaseMS <= 0 {
		t.Fatal("code expiry killed independent session", got.Code)
	}
}
