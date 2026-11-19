package relay

import (
	"context"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/accesscode"
	"github.com/xallor-ai/xallor-remote/internal/delegation"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Failed durable expiry settlement must not imply the DB row ended or permit
// another lease. The original route remains until the real target terminal.
func TestSessionExecutionLease_shouldDenyExpiredConsent_whenDurableEndFails(t *testing.T) {
	s, h, target, _, request, route := sessionExecutionFixture(t)
	if _, err := s.faults.Exec(`UPDATE session_authorizations SET expires_at=? WHERE session_id=?`, time.Now().Add(-time.Second).Unix(), request.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.faults.Exec(`CREATE TRIGGER deny_session_end BEFORE UPDATE OF state ON session_authorizations WHEN NEW.state='ended' BEGIN SELECT RAISE(ABORT,'fixture refusal'); END;`); err != nil {
		t.Fatal(err)
	}
	h.Handle(target, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeExecutionRenewRoute, RequestID: "renew", ExecID: route})
	if got := target.last(); got.Code != protocol.Unauthorized || got.LeaseMS != 0 || h.inflight[route] == nil {
		t.Fatal("failed persistence granted lease or erased route")
	}
	if err := s.ReadAccessCode(context.Background(), func(r accesscode.Reader) error {
		ss, err := r.Session(context.Background(), request.SessionID)
		if err != nil {
			return err
		}
		if ss.State != accesscode.SessionActive || ss.CanOperate(time.Now()) == nil {
			t.Error("unconfirmed end disguised as durable success")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// A known expired/revoked session detaches only that consent, allowing the
// same controller to explicitly obtain a new approval while old tasks settle.
func TestSessionExecutionLease_shouldAllowFreshApproval_afterOldConsentDenied(t *testing.T) {
	s, h, target, client, request, route := sessionExecutionFixture(t)
	if err := s.WriteAccessCode(context.Background(), func(tx accesscode.Transaction) error {
		_, err := tx.RevokeSession(context.Background(), request.SessionID, time.Now())
		return err
	}); err != nil {
		t.Fatal(err)
	}
	h.Handle(target, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeExecutionRenewRoute, RequestID: "old", ExecID: route})
	if h.clients[client].session != nil || !h.clients[client].sessionRequired || client.closed {
		t.Fatal("invalid consent not detached safely")
	}
	next := nextSessionRequest(t, s, h, client, "fresh-lease", 3, protocol.OpExec)
	approveSession(t, h, target, client, next)
	if client.last().Status != protocol.SessionActive {
		t.Fatal("same controller could not reapply")
	}
	h.Handle(client, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeInvoke, ExecID: "new", SessionID: next.SessionID, Op: protocol.OpExec})
	newRoute := target.last().ExecID
	h.Handle(target, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeExecutionRenewRoute, RequestID: "fresh", ExecID: newRoute})
	if target.last().Code != "" {
		t.Fatal("new consent could not renew")
	}
	if h.inflight[route] == nil || !h.inflight[route].cancelled {
		t.Fatal("new approval replaced old route")
	}
}

type invalidSessionLeaseAuthority struct {
	ExecutionAuthority
	ttl time.Duration
}

type timeoutSessionEndStore struct {
	accesscode.Store
	entered chan struct{}
}

func (s timeoutSessionEndStore) WriteAccessCode(ctx context.Context, _ func(accesscode.Transaction) error) error {
	close(s.entered)
	<-ctx.Done()
	return ctx.Err()
}

// A known expiry is reported and detached before best-effort durable cleanup.
// Waiting out the cleanup deadline cannot downgrade denial or strand the client.
func TestSessionExecutionLease_shouldDetachBeforeCleanup_whenExpiryPersistenceTimesOut(t *testing.T) {
	s, h, target, client, request, route := sessionExecutionFixture(t)
	if _, err := s.faults.Exec(`UPDATE session_authorizations SET expires_at=? WHERE session_id=?`, time.Now().Add(-time.Second).Unix(), request.SessionID); err != nil {
		t.Fatal(err)
	}
	waiting := timeoutSessionEndStore{Store: s.SQLStore, entered: make(chan struct{})}
	h.accessCodes = waiting
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.Handle(target, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeExecutionRenewRoute, RequestID: "expire", ExecID: route})
	}()
	select {
	case <-waiting.entered:
	case <-time.After(time.Second):
		t.Fatal("cleanup was not attempted")
	}
	if got := target.last(); got.Type != protocol.TypeExecutionLease || got.Code != protocol.Unauthorized {
		t.Errorf("known denial not published before cleanup: %s/%s", got.Type, got.Code)
	}
	h.mu.Lock()
	detached := h.clients[client].session == nil && h.clients[client].sessionRequired
	cancelled := h.inflight[route].cancelled
	h.mu.Unlock()
	if !detached || !cancelled {
		t.Error("expiry did not detach before cleanup")
	}
	<-done
	if target.last().Code != protocol.Unauthorized {
		t.Error("cleanup timeout downgraded known denial")
	}
	h.accessCodes = s.SQLStore
	next := nextSessionRequest(t, s, h, client, "after-end-timeout", 3)
	approveSession(t, h, target, client, next)
	if client.last().Status != protocol.SessionActive {
		t.Error("cleanup failure stranded same controller")
	}
}

func (a invalidSessionLeaseAuthority) RenewForeground(context.Context, delegation.Invocation) (time.Duration, error) {
	return a.ttl, nil
}

// Intersecting with a shorter session may not sanitize an invalid provider TTL
// into a valid grant; a faulty authority response remains uncertain and bounded.
func TestSessionExecutionLease_shouldRejectInvalidProviderBudget_beforeClamping(t *testing.T) {
	for _, ttl := range []time.Duration{0, time.Minute} {
		_, h, target, _, _, route := sessionExecutionFixture(t)
		h.executionAuthority = invalidSessionLeaseAuthority{h.executionAuthority, ttl}
		h.Handle(target, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeExecutionRenewRoute, RequestID: "invalid", ExecID: route})
		if got := target.last(); got.Code != protocol.RelayError || got.LeaseMS != 0 {
			t.Fatal("invalid provider lease accepted")
		}
	}
}
