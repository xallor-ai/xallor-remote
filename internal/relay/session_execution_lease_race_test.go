package relay

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/accesscode"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

type unavailableSessionReader struct{ accesscode.Store }

func (s unavailableSessionReader) ReadAccessCode(context.Context, func(accesscode.Reader) error) error {
	return errors.New("fixture session read unavailable")
}

// A transient SQL failure must not extend authority or permanently poison a
// route; explicit denial is permanent even if a row is later restored.
func TestSessionExecutionLease_shouldDistinguishTransientReadFailure_fromDefiniteDenial(t *testing.T) {
	s, h, target, _, request, route := sessionExecutionFixture(t)
	h.accessCodes = unavailableSessionReader{Store: s.SQLStore}
	renew := func() {
		h.Handle(target, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeExecutionRenewRoute, RequestID: "renew", ExecID: route})
	}
	renew()
	if got := target.last(); got.Code != protocol.RelayError || got.LeaseMS != 0 || h.inflight[route].cancelled {
		t.Fatal("SQL uncertainty treated as active or permanent denial")
	}
	h.accessCodes = s.SQLStore
	renew()
	if target.last().Code != "" {
		t.Fatal("SQL recovery could not renew")
	}
	if err := s.WriteAccessCode(context.Background(), func(tx accesscode.Transaction) error {
		_, err := tx.RevokeSession(context.Background(), request.SessionID, time.Now())
		return err
	}); err != nil {
		t.Fatal(err)
	}
	renew()
	if target.last().Code != protocol.Unauthorized {
		t.Fatal("revocation not observed")
	}
	expires := h.inflight[route].binding.session.ExpiresAt.Unix()
	if _, err := s.faults.Exec(`UPDATE session_authorizations SET state='active',expires_at=? WHERE session_id=?`, expires, request.SessionID); err != nil {
		t.Fatal(err)
	}
	renew()
	if target.last().Code != protocol.Unauthorized || !h.inflight[route].cancelled {
		t.Fatal("denied invocation revived")
	}
}

// Revoke during a slow base-authority check must be observed by the subsequent
// session check, without waiting on the routing mutex or losing the stream.
func TestSessionExecutionLease_shouldRecheckConsent_afterBaseAuthorityWait(t *testing.T) {
	s, h, target, _, request, route := sessionExecutionFixture(t)
	waiting := waitingExecutionAuthority{h.executionAuthority, make(chan struct{}), make(chan struct{})}
	h.executionAuthority = waiting
	done := make(chan struct{})
	var once sync.Once
	unpause := func() { once.Do(func() { close(waiting.resume) }) }
	go func() {
		defer close(done)
		h.Handle(target, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeExecutionRenewRoute, RequestID: "late", ExecID: route})
	}()
	defer func() { unpause(); <-done }()
	select {
	case <-waiting.entered:
	case <-time.After(time.Second):
		t.Fatal("authority barrier missing")
	}
	if err := s.WriteAccessCode(context.Background(), func(tx accesscode.Transaction) error {
		_, err := tx.RevokeCode(context.Background(), request.CodeID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	unpause()
	<-done
	if got := target.last(); got.Code != protocol.Unauthorized || got.LeaseMS != 0 {
		t.Fatal("late base success ignored revocation")
	}
}

// Expiry after the durable read but before publication must still deny. The
// elapsed routing wait cannot be converted into a fresh positive TTL.
func TestSessionExecutionLease_shouldRejectExpiry_whileWaitingForPublication(t *testing.T) {
	s, h, target, _, request, route := sessionExecutionFixture(t)
	expires := time.Now().Add(2 * time.Second).Truncate(time.Second)
	if _, err := s.faults.Exec(`UPDATE session_authorizations SET expires_at=? WHERE session_id=?`, expires.Unix(), request.SessionID); err != nil {
		t.Fatal(err)
	}
	h.inflight[route].binding.session.ExpiresAt = &expires
	barrier := &targetLifecycleBarrier{Store: s.SQLStore, phase: "after-invoke-read", entered: make(chan struct{}), release: make(chan struct{})}
	h.accessCodes = barrier
	done := make(chan struct{})
	var once sync.Once
	unpause := func() { once.Do(func() { close(barrier.release) }) }
	go func() {
		defer close(done)
		h.Handle(target, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeExecutionRenewRoute, RequestID: "expiry", ExecID: route})
	}()
	defer func() { unpause(); <-done }()
	select {
	case <-barrier.entered:
	case <-time.After(time.Second):
		t.Fatal("session read barrier missing")
	}
	h.mu.Lock()
	unpause()
	if wait := time.Until(expires.Add(10 * time.Millisecond)); wait > 0 {
		time.Sleep(wait)
	}
	h.mu.Unlock()
	<-done
	if target.last().Code != protocol.Unauthorized || target.last().LeaseMS != 0 {
		t.Fatal("expired publication granted lease")
	}
	if err := s.ReadAccessCode(context.Background(), func(r accesscode.Reader) error {
		ss, err := r.Session(context.Background(), request.SessionID)
		if ss.State != accesscode.SessionEnded {
			t.Errorf("expiry not persisted: %s", ss.State)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

// An old execution cannot borrow a newly adopted session's authority, and its
// denial/terminal cannot cancel the new route on the same controller socket.
func TestSessionExecutionLease_shouldKeepOriginalConsent_whenControllerAdoptsNewSession(t *testing.T) {
	s, h, target, client, oldRequest, oldRoute := sessionExecutionFixture(t)
	second := &recSender{}
	next := nextSessionRequest(t, s, h, second, "second-lease", 3, protocol.OpExec)
	approveSession(t, h, target, second, next)
	active := second.last()
	h.Handle(client, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeSessionResumeRequest, RequestID: "adopt", SessionID: active.SessionID, ResumeID: active.ResumeID, ResumeCapability: active.ResumeCapability, TargetUID: next.TargetUID})
	if client.last().Status != protocol.SessionActive {
		t.Fatal("new session not adopted")
	}
	if err := s.WriteAccessCode(context.Background(), func(tx accesscode.Transaction) error {
		_, err := tx.RevokeSession(context.Background(), oldRequest.SessionID, time.Now())
		return err
	}); err != nil {
		t.Fatal(err)
	}
	h.Handle(target, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeExecutionRenewRoute, RequestID: "old", ExecID: oldRoute})
	if target.last().Code != protocol.Unauthorized {
		t.Fatal("old task borrowed new session")
	}
	h.Handle(client, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeInvoke, ExecID: "new-run", SessionID: next.SessionID, Op: protocol.OpExec})
	newRoute := target.last().ExecID
	if h.inflight[newRoute] == nil {
		t.Fatal("new task not admitted")
	}
	h.Handle(target, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeExecutionRenewRoute, RequestID: "new", ExecID: newRoute})
	if target.last().Code != "" {
		t.Fatal("old denial poisoned new session")
	}
	h.Handle(target, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeExit, ExecID: oldRoute, Status: protocol.ExitAuthorizationLost})
	if h.inflight[newRoute] == nil || h.admission.Active(next.TargetUID) != 1 {
		t.Fatal("old terminal destroyed new task")
	}
}
