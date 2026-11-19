package relay

import (
	"context"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/accesscode"
	"github.com/xallor-ai/xallor-remote/internal/delegation"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Explicit code revocation fences matching in-memory consent and routes without
// releasing admission or synthesizing a target terminal.
func TestCodeSessionFence_shouldCancelOnlyMatchingGeneration_andKeepRouteUntilTerminal(t *testing.T) {
	s, h, target, client := approvedSessionFixture(t)
	h.executionAuthority = delegation.NewAuthority([32]byte{1}, s, nil, nil)
	active := client.last()
	var approved accesscode.SessionAuthorization
	if err := s.ReadAccessCode(context.Background(), func(r accesscode.Reader) error {
		var err error
		approved, err = r.Session(context.Background(), active.SessionID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	approved.Scope = []string{protocol.OpInfo}
	h.Handle(client, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeInvoke, ExecID: "fence-run", SessionID: active.SessionID, Op: protocol.OpInfo})
	route := target.last().ExecID
	uid := h.devices["device"].record.UID
	h.mu.Lock()
	h.invalidateCodeSessionsLocked(uid, approved.CodeID, approved.Generation)
	h.mu.Unlock()
	if h.inflight[route] == nil || !h.inflight[route].cancelled || h.admission.Active(uid) != 1 {
		t.Fatalf("matching code fence released route/capacity route=%s inflight=%#v active=%d session=%#v approved=%#v", route, h.inflight[route], h.admission.Active(uid), h.clients[client].session, approved)
	}
	if h.clients[client].session != nil || !h.clients[client].sessionRequired {
		t.Fatal("matching consent remained or fallback opened")
	}
	other := approved
	other.ID = "other-session"
	other.CodeID = "other-code"
	h.mu.Lock()
	slot := h.clients[client]
	slot.sessionRequired = true
	slot.session = &other
	h.clients[client] = slot
	h.mu.Unlock()
	h.mu.Lock()
	h.invalidateCodeSessionsLocked(uid, approved.CodeID, approved.Generation)
	h.mu.Unlock()
	if h.clients[client].session == nil || h.clients[client].session.CodeID != other.CodeID {
		t.Fatal("unrelated code session was fenced")
	}
	h.Handle(target, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeExit, ExecID: route, Status: protocol.ExitAuthorizationLost})
	if h.admission.Active(uid) != 0 {
		t.Fatal("terminal did not release route")
	}
}
