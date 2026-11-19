package relay

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/accesscode"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

type targetLifecycleBarrier struct {
	accesscode.Store
	phase            string
	entered, release chan struct{}
	once             sync.Once
}

func (b *targetLifecycleBarrier) pause(ctx context.Context) {
	first := false
	b.once.Do(func() { first = true; close(b.entered) })
	if first {
		select {
		case <-b.release:
		case <-ctx.Done():
		}
	}
}
func (b *targetLifecycleBarrier) WriteAccessCode(ctx context.Context, fn func(accesscode.Transaction) error) error {
	if b.phase == "before-rotation" {
		b.pause(ctx)
	}
	err := b.Store.WriteAccessCode(ctx, fn)
	if err == nil && b.phase == "after-rotation" {
		b.pause(ctx)
	}
	return err
}
func (b *targetLifecycleBarrier) ReadAccessCode(ctx context.Context, fn func(accesscode.Reader) error) error {
	err := b.Store.ReadAccessCode(ctx, fn)
	if err == nil && b.phase == "after-invoke-read" {
		b.pause(ctx)
	}
	return err
}

// DB rotation and in-memory publication have a window between them. Old
// invocations must be fenced before, during and after that window.
func TestSessionTarget_shouldFenceOldInvocation_acrossReplacementWindows(t *testing.T) {
	for _, phase := range []string{"before-rotation", "after-rotation", "after-invoke-read"} {
		t.Run(phase, func(t *testing.T) {
			s, h, _, client := approvedSessionFixture(t)
			active := client.last()
			barrier := &targetLifecycleBarrier{Store: s.SQLStore, phase: phase, entered: make(chan struct{}), release: make(chan struct{})}
			h.accessCodes = barrier
			var once sync.Once
			unpause := func() { once.Do(func() { close(barrier.release) }) }
			done := make(chan struct{})
			replacement := &recSender{}
			invoke := func() {
				h.Handle(client, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeInvoke, ExecID: "old-invoke", SessionID: active.SessionID, Op: protocol.OpInfo})
			}
			register := func() {
				if code := h.RegisterDevice("device", "secret", true, replacement); code != "" {
					t.Error(code)
				}
			}
			go func() {
				defer close(done)
				if phase == "after-invoke-read" {
					invoke()
				} else {
					register()
				}
			}()
			defer func() { unpause(); <-done }()
			select {
			case <-barrier.entered:
			case <-time.After(3 * time.Second):
				t.Fatal("barrier not reached")
			}
			if phase == "after-invoke-read" {
				register()
			} else {
				invoke()
			}
			unpause()
			<-done
			if got := client.last(); got.Type != protocol.TypeInvokeNack {
				t.Errorf("old request crossed replacement: %s", got.Type)
			}
			h.mu.Lock()
			routes := len(h.inflight)
			h.mu.Unlock()
			if routes != 0 {
				t.Error("old invocation routed")
			}
			assertRetiredTargetConsent(t, s, active)
		})
	}
}

// A delayed Drop for an old socket cannot retire a replacement's new consent.
func TestSessionTarget_shouldPreserveNewConsent_whenOldDropArrivesLate(t *testing.T) {
	s, h, old, original := approvedSessionFixture(t)
	h.Drop(original, protocol.RoleClient, "device")
	next := &recSender{}
	if code := h.RegisterDevice("device", "secret", true, next); code != "" {
		t.Fatal(code)
	}
	h.Handle(next, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeDeviceSettings, RequestID: "ready", GrantHash: protocol.SHA256Hex("xr_grant_transport"), Inbound: protocol.Bool(true)})
	client := &recSender{}
	if code := h.RegisterClient("device", "xr_grant_transport", "", client); code != "" {
		t.Fatal(code)
	}
	now := time.Now().UTC()
	code := accesscode.Code{ID: "new-code", DeviceUID: h.devices["device"].record.UID, Type: accesscode.OneTime, Generation: 2, IssuedAt: now, Scope: []string{protocol.OpInfo}, RequireApproval: true, State: accesscode.StateActive}
	if err := s.WriteAccessCode(context.Background(), func(tx accesscode.Transaction) error {
		return tx.InsertCode(context.Background(), code, protocol.SHA256Hex("new-secret"))
	}); err != nil {
		t.Fatal(err)
	}
	h.Handle(client, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeSessionRequest, RequestID: "new-request", SessionID: "new-session", TargetUID: code.DeviceUID, CodeID: code.ID, CodeGeneration: 2, SessionScope: code.Scope, Grant: "new-secret"})
	approval := next.last()
	h.Handle(next, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeSessionApprovalResult, RequestID: approval.RequestID, SessionID: approval.SessionID, RequestSHA256: approval.RequestSHA256, Allow: protocol.Bool(true)})
	active := client.last()
	if active.Status != protocol.SessionActive {
		t.Fatal("new session not approved")
	}
	h.Drop(old, protocol.RoleDevice, "device")
	if err := s.ReadAccessCode(context.Background(), func(r accesscode.Reader) error {
		ss, err := r.Session(context.Background(), active.SessionID)
		if err != nil {
			return err
		}
		return ss.CanOperate(time.Now())
	}); err != nil {
		t.Fatal(err)
	}
	h.Handle(client, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeInvoke, ExecID: "new-invoke", SessionID: active.SessionID, Op: protocol.OpInfo})
	if next.last().Type != protocol.TypeInvoke {
		t.Fatal("stale drop stopped replacement")
	}
}
