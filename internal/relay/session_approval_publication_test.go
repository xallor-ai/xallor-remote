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

type rejectApprovedSender struct{ *recSender }

func (s *rejectApprovedSender) Enqueue(m protocol.Message) error {
	if m.Type == protocol.TypeSessionResult && m.Status == protocol.SessionActive {
		return errors.New("fixture active refusal")
	}
	return s.recSender.Enqueue(m)
}

// Consent consumption can commit just before target/client loss, pending
// replacement or queue refusal. Publication must not rebind stale state or
// refund the code; a replacement pending record must remain untouched.
func TestSessionApproval_shouldRetireUnpublishedConsent_whenStateChangesAfterCommit(t *testing.T) {
	for _, change := range []string{"target-drop", "target-replace", "client-drop", "pending-replaced", "client-changed", "queue-refused"} {
		t.Run(change, func(t *testing.T) {
			s, h, target, _ := approvedSessionFixture(t)
			recorder := &recSender{}
			var client Sender = recorder
			if change == "queue-refused" {
				client = &rejectApprovedSender{recSender: recorder}
			}
			request := nextSessionRequest(t, s, h, client, "approval", 2)
			h.Handle(client, protocol.RoleClient, "device", request)
			approval := target.last()
			original := h.pendingSessions[request.SessionID]
			if original == nil {
				t.Fatal("missing pending approval")
			}
			barrier := &resumeCommitBarrier{Store: s.SQLStore, entered: make(chan struct{}), release: make(chan struct{})}
			h.accessCodes = barrier
			var once sync.Once
			unpause := func() { once.Do(func() { close(barrier.release) }) }
			done := make(chan struct{})
			go func() {
				defer close(done)
				h.Handle(target, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeSessionApprovalResult, RequestID: approval.RequestID, SessionID: approval.SessionID, RequestSHA256: approval.RequestSHA256, Allow: protocol.Bool(true)})
			}()
			defer func() { unpause(); <-done }()
			select {
			case <-barrier.entered:
			case <-time.After(3 * time.Second):
				t.Fatal("approval commit barrier not reached")
			}
			var replacement *pendingSession
			switch change {
			case "target-drop":
				h.Drop(target, protocol.RoleDevice, "device")
			case "target-replace":
				if code := h.RegisterDevice("device", "secret", true, &recSender{}); code != "" {
					t.Fatal(code)
				}
			case "client-drop":
				h.Drop(client, protocol.RoleClient, "device")
			case "pending-replaced":
				copy := *original
				replacement = &copy
				replacement.reservationID = "replacement"
				h.mu.Lock()
				h.pendingSessions[request.SessionID] = replacement
				h.mu.Unlock()
			case "client-changed":
				h.mu.Lock()
				slot := h.clients[client]
				slot.sessionRequired = false
				h.clients[client] = slot
				h.mu.Unlock()
			}
			unpause()
			<-done
			got := recorder.last()
			if got.Status != protocol.SessionUnknownState || got.ResumeCapability != "" {
				t.Errorf("unpublished consent reported success/proof: %s", got.Status)
			}
			if h.clients[client].session != nil {
				t.Error("stale session bound")
			}
			if replacement != nil && h.pendingSessions[request.SessionID] != replacement {
				t.Error("new pending object deleted")
			}
			if change == "client-changed" && h.clients[client].sessionRequired {
				t.Error("new client state overwritten")
			}
			if err := s.ReadAccessCode(context.Background(), func(r accesscode.Reader) error {
				ss, err := r.Session(context.Background(), request.SessionID)
				if err != nil {
					return err
				}
				code, err := r.Code(context.Background(), request.CodeID)
				if err != nil {
					return err
				}
				if ss.State != accesscode.SessionRevoked || code.State != accesscode.StateConsumed {
					t.Errorf("retirement/refund mismatch: session=%s code=%s", ss.State, code.State)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
