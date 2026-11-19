package relay

import (
	"context"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/accesscode"
	"github.com/xallor-ai/xallor-remote/internal/delegation"
)

// Background delegation proof is bound to the original logical session. Code
// revoke/session revoke invalidates renewal without changing receipt state.
func TestHandoffDelegation_shouldRecheckOriginalSession_whenCodeIsRevoked(t *testing.T) {
	s, h, _, client := approvedSessionFixture(t)
	key, err := s.ExecutionKey()
	if err != nil {
		t.Fatal(err)
	}
	authority := delegation.NewAuthority(key, s, nil, nil)
	authority.SetSessionStore(s.SQLStore)
	h.executionAuthority = authority
	active := client.last()
	if _, err := s.faults.Exec(`UPDATE session_authorizations SET scope_json='["handoff.submit"]' WHERE session_id=?`, active.SessionID); err != nil {
		t.Fatal(err)
	}
	var session accesscode.SessionAuthorization
	if err := s.ReadAccessCode(context.Background(), func(r accesscode.Reader) error {
		var err error
		session, err = r.Session(context.Background(), active.SessionID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	slot := h.clients[client]
	h.mu.Unlock()
	proof, err := authority.IssueSession("device", slot.access, slot.identity, slot.authorization, session)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authority.Renew(context.Background(), "device", h.devices["device"].record.UID, proof); err != nil {
		t.Fatal("fresh proof denied", err)
	}
	if err := s.WriteAccessCode(context.Background(), func(tx accesscode.Transaction) error {
		_, err := tx.RevokeSession(context.Background(), session.ID, time.Now())
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := authority.Renew(context.Background(), "device", h.devices["device"].record.UID, proof); err == nil {
		t.Fatal("revoked session proof renewed")
	}
}
