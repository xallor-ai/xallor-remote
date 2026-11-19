package relay

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/accesscode"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// A committed consumption cannot be refunded. If its route disappears before
// publication, return no active result/proof and retire the undelivered rotation.
func TestSessionResumePublish_shouldNotActivateChangedRoute_afterCommit(t *testing.T) {
	for _, change := range []string{"client-drop", "target-drop", "target-replace", "inbound-off", "queue-reject", "client-cancel", "target-cancel", "queue-reject-cleanup-failure"} {
		t.Run(change, func(t *testing.T) {
			s, h, target, original := approvedSessionFixture(t)
			active := original.last()
			h.Drop(original, protocol.RoleClient, "device")
			recorder := &recSender{}
			var fresh Sender = recorder
			if strings.HasPrefix(change, "queue-reject") {
				fresh = &rejectResumeActiveSender{recSender: recorder}
			}
			connectionContext, cancelConnection := context.WithCancel(context.Background())
			defer cancelConnection()
			if change == "client-cancel" {
				fresh = &resumeContextSender{recSender: recorder, ctx: connectionContext}
			}
			if code := h.RegisterClient("device", "xr_grant_transport", "", fresh); code != "" {
				t.Fatal(code)
			}
			h.mu.Lock()
			uid := h.devices["device"].record.UID
			if change == "target-cancel" {
				h.devices["device"].conn = &resumeContextSender{recSender: target, ctx: connectionContext}
			}
			h.mu.Unlock()
			barrier := &resumeCommitBarrier{Store: s.SQLStore, entered: make(chan struct{}), release: make(chan struct{})}
			h.accessCodes = barrier
			done := make(chan struct{})
			var released sync.Once
			unpause := func() { released.Do(func() { close(barrier.release) }) }
			go func() {
				defer close(done)
				h.Handle(fresh, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeSessionResumeRequest, RequestID: "publish-resume", SessionID: active.SessionID, ResumeID: active.ResumeID, ResumeCapability: active.ResumeCapability, TargetUID: uid})
			}()
			defer func() { unpause(); <-done }()
			select {
			case <-barrier.entered:
			case <-time.After(3 * time.Second):
				t.Fatal("consume never committed")
			}
			var rotatedID string
			if err := s.ReadAccessCode(context.Background(), func(r accesscode.Reader) error {
				rotated, err := r.ActiveResumeForSession(context.Background(), active.SessionID)
				rotatedID = rotated.ID
				return err
			}); err != nil {
				t.Fatal(err)
			}
			switch change {
			case "client-drop":
				h.Drop(fresh, protocol.RoleClient, "device")
			case "target-drop":
				h.Drop(target, protocol.RoleDevice, "device")
			case "target-replace":
				replacement := &recSender{}
				if code := h.RegisterDevice("device", "secret", true, replacement); code != "" {
					t.Fatal(code)
				}
				h.Handle(replacement, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeDeviceSettings, RequestID: "ready", GrantHash: protocol.SHA256Hex("xr_grant_transport"), Inbound: protocol.Bool(true)})
			case "inbound-off":
				h.Handle(target, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeDeviceSettings, RequestID: "off", GrantHash: protocol.SHA256Hex("xr_grant_transport"), Inbound: protocol.Bool(false)})
			case "client-cancel", "target-cancel":
				cancelConnection()
			case "queue-reject-cleanup-failure":
				if _, err := s.faults.Exec(`CREATE TRIGGER deny_resume_retirement BEFORE UPDATE OF state ON session_authorizations WHEN NEW.state='revoked' BEGIN SELECT RAISE(ABORT,'fixture cleanup failure'); END;`); err != nil {
					t.Fatal(err)
				}
			}
			unpause()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("resume publication did not settle")
			}
			result := recorder.last()
			if result.Status == protocol.SessionActive || result.ResumeCapability != "" {
				t.Fatalf("changed route published active/proof: status=%s", result.Status)
			}
			if result.Type != protocol.TypeSessionResumeResult || result.Status != protocol.SessionUnknownState || result.Code != protocol.SessionUnknown {
				t.Fatal("unpublished commit was not reported as unknown")
			}
			h.mu.Lock()
			bound := h.clients[fresh].session != nil
			h.mu.Unlock()
			if bound {
				t.Fatal("unpublished session became routable")
			}
			if err := s.ReadAccessCode(context.Background(), func(r accesscode.Reader) error {
				old, err := r.Resume(context.Background(), active.ResumeID)
				if err != nil {
					return err
				}
				if old.State != accesscode.ResumeConsumed {
					return fmt.Errorf("committed consumption was refunded")
				}
				next, err := r.Resume(context.Background(), rotatedID)
				if err != nil {
					return err
				}
				wantProof, wantSession := accesscode.ResumeRevoked, accesscode.SessionRevoked
				if change == "queue-reject-cleanup-failure" {
					wantProof, wantSession = accesscode.ResumeActive, accesscode.SessionActive
				}
				if next.State != wantProof {
					return fmt.Errorf("undelivered proof stayed active")
				}
				session, err := r.Session(context.Background(), active.SessionID)
				if err != nil {
					return err
				}
				if session.State != wantSession {
					return fmt.Errorf("unpublished session stayed active")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

type resumeContextSender struct {
	*recSender
	ctx context.Context
}

func (s *resumeContextSender) Context() context.Context { return s.ctx }

type resumeCommitBarrier struct {
	accesscode.Store
	entered, release chan struct{}
	once             sync.Once
}

func (s *resumeCommitBarrier) WriteAccessCode(ctx context.Context, fn func(accesscode.Transaction) error) error {
	err := s.Store.WriteAccessCode(ctx, fn)
	if err == nil {
		first := false
		s.once.Do(func() {
			first = true
			close(s.entered)
		})
		if first {
			select {
			case <-s.release:
			case <-ctx.Done():
			}
		}
	}
	return err
}

type rejectResumeActiveSender struct{ *recSender }

func (s *rejectResumeActiveSender) Enqueue(message protocol.Message) error {
	if message.Type == protocol.TypeSessionResumeResult && message.Status == protocol.SessionActive {
		return errors.New("fixture queue refusal")
	}
	return s.recSender.Enqueue(message)
}
