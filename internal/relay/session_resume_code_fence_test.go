package relay

import (
	"context"
	"sync"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/accesscode"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Resume and owner management arrive on different connections. A committed
// resume paused before publication must not restore consent revoked meanwhile.
func TestSessionResume_shouldRejectUnpublishedConsent_whenCodeManagementCommits(t *testing.T) {
	for _, test := range []struct {
		name, action, codeID string
		revoked              bool
	}{
		{"revoke-origin", "code.revoke", "code-resume-fence", true},
		{"rotate-origin", "code.rotate", "code-resume-fence", true},
		{"revoke-unrelated", "code.revoke", "managed-code", false},
		{"rotate-unrelated", "code.rotate", "managed-code", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, h, target, original, device, token := typedCodeFixture(t)
			active := approveManagedExecCode(t, s, h, target, original, device, "resume-fence", 2)
			h.Drop(original, protocol.RoleClient, "device")
			fresh := &recSender{}
			if code := h.RegisterClient("device", "", token, fresh); code != "" {
				t.Fatal("replacement client registration failed", code)
			}
			barrier := &resumeCommitBarrier{Store: s.SQLStore, entered: make(chan struct{}), release: make(chan struct{})}
			h.accessCodes = barrier
			var once sync.Once
			unpause := func() { once.Do(func() { close(barrier.release) }) }
			done := make(chan struct{})
			go func() {
				defer close(done)
				h.Handle(fresh, protocol.RoleClient, "device", protocol.Message{
					Type: protocol.TypeSessionResumeRequest, RequestID: "resume-fenced", SessionID: active.SessionID,
					ResumeID: active.ResumeID, ResumeCapability: active.ResumeCapability, TargetUID: device.UID,
				})
			}()
			defer func() { unpause(); <-done }()
			waitSignal(t, barrier.entered)
			req := access.Request{Action: test.action, CodeID: test.codeID}
			if test.action == "code.rotate" {
				req.NewCodeID, req.NewSecret = "replacement-resume-fence", "replacement-secret"
			}
			if result := sendTypedAccess(t, h, target, token, req); result.Code != "" {
				t.Fatal("owner mutation failed", result.Code)
			}
			unpause()
			waitSignal(t, done)
			result := fresh.last()
			if test.revoked {
				if result.Status != protocol.SessionUnknownState || result.ResumeCapability != "" {
					t.Errorf("revoked committed resume published success/proof: status=%s", result.Status)
				}
			} else if result.Status != protocol.SessionActive || result.ResumeCapability == "" {
				t.Errorf("unrelated code management blocked valid resume: status=%s", result.Status)
			}
			h.mu.Lock()
			bound := h.clients[fresh].session != nil
			h.mu.Unlock()
			if bound == test.revoked {
				t.Errorf("wrong replacement client session binding: bound=%v revoked=%v", bound, test.revoked)
			}
			if err := s.ReadAccessCode(context.Background(), func(r accesscode.Reader) error {
				old, err := r.Resume(context.Background(), active.ResumeID)
				if err != nil {
					return err
				}
				if old.State != accesscode.ResumeConsumed {
					t.Error("committed resume consumption was refunded")
				}
				session, err := r.Session(context.Background(), active.SessionID)
				want := accesscode.SessionActive
				if test.revoked {
					want = accesscode.SessionRevoked
				}
				if err == nil && session.State != want {
					t.Errorf("wrong durable session: got=%s want=%s", session.State, want)
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
