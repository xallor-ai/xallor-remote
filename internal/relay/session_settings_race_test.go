package relay

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/accesscode"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Verifying the old session before a complete off/on cycle cannot authorize
// admission after that cycle clears the controller's session pointer.
func TestSessionSettings_shouldRejectOldInvoke_whenVerificationCrossesOffOn(t *testing.T) {
	s, h, target, client := approvedSessionFixture(t)
	active := client.last()
	barrier := &targetLifecycleBarrier{Store: s.SQLStore, phase: "after-invoke-read", entered: make(chan struct{}), release: make(chan struct{})}
	h.accessCodes = barrier
	var once sync.Once
	unpause := func() { once.Do(func() { close(barrier.release) }) }
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.Handle(client, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeInvoke, ExecID: "crossed", SessionID: active.SessionID, Op: protocol.OpInfo})
	}()
	defer func() { unpause(); <-done }()
	select {
	case <-barrier.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("read barrier not reached")
	}
	toggleSessionInbound(h, target, false)
	toggleSessionInbound(h, target, true)
	unpause()
	<-done
	if got := client.last(); got.Type != protocol.TypeInvokeNack || got.Code != protocol.Unauthorized {
		t.Fatal("stale verification admitted", got.Type, got.Code)
	}
	if len(h.inflight) != 0 {
		t.Fatal("old invocation routed")
	}
}

// Retired connections already have required=true. Two successful reservations
// therefore need explicit per-client exclusion at the final publication gate.
func TestSessionSettings_shouldPublishOnePending_whenRetiredClientRequestsConcurrently(t *testing.T) {
	s, h, target, client := approvedSessionFixture(t)
	toggleSessionInbound(h, target, false)
	toggleSessionInbound(h, target, true)
	a := nextSessionRequest(t, s, h, client, "a", 2)
	b := nextSessionRequest(t, s, h, client, "b", 3)
	barrier := &resumeCommitBarrier{Store: s.SQLStore, entered: make(chan struct{}), release: make(chan struct{})}
	h.accessCodes = barrier
	var once sync.Once
	unpause := func() { once.Do(func() { close(barrier.release) }) }
	done := make(chan struct{})
	go func() { defer close(done); h.Handle(client, protocol.RoleClient, "device", a) }()
	defer func() { unpause(); <-done }()
	select {
	case <-barrier.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("reserve barrier not reached")
	}
	h.Handle(client, protocol.RoleClient, "device", b)
	winner := h.pendingSessions[b.SessionID]
	if winner == nil {
		t.Fatal("winner missing")
	}
	unpause()
	<-done
	if len(h.pendingSessions) != 1 || h.pendingSessions[b.SessionID] != winner || h.pendingSessions[a.SessionID] != nil || !h.clients[client].sessionRequired {
		t.Fatal("parallel consent requests shared one client")
	}
	if err := s.ReadAccessCode(context.Background(), func(r accesscode.Reader) error {
		record, err := r.Reservation(context.Background(), a.RequestID)
		if record.State != accesscode.ReservationReleased {
			t.Error("loser reservation not released")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

type settingsUpdateBarrier struct {
	DeviceStore
	before           bool
	entered, release chan struct{}
}

func (b *settingsUpdateBarrier) UpdateDevice(ctx context.Context, id string, record access.Device) (access.Device, error) {
	pause := func() {
		close(b.entered)
		select {
		case <-b.release:
		case <-ctx.Done():
		}
	}
	if b.before {
		pause()
	}
	result, err := b.DeviceStore.UpdateDevice(ctx, id, record)
	if !b.before && err == nil {
		pause()
	}
	return result, err
}

// While a settings transaction is pending, new admission is fenced without
// removing the target transport. Drop after commit retires the returned epoch.
func TestSessionSettings_shouldFenceMutationAndRetireCommit_whenTargetDrops(t *testing.T) {
	for _, before := range []bool{true, false} {
		name := "after-commit"
		if before {
			name = "before-write"
		}
		t.Run(name, func(t *testing.T) {
			s, h, target, client := approvedSessionFixture(t)
			active := client.last()
			if !before {
				toggleSessionInbound(h, target, false)
			}
			barrier := &settingsUpdateBarrier{DeviceStore: s, before: before, entered: make(chan struct{}), release: make(chan struct{})}
			h.store = barrier
			var once sync.Once
			unpause := func() { once.Do(func() { close(barrier.release) }) }
			done := make(chan struct{})
			go func() { defer close(done); toggleSessionInbound(h, target, !before) }()
			defer func() { unpause(); <-done }()
			select {
			case <-barrier.entered:
			case <-time.After(3 * time.Second):
				t.Fatal("update barrier not reached")
			}
			if before {
				h.Handle(client, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeInvoke, ExecID: "during-write", SessionID: active.SessionID, Op: protocol.OpInfo})
				if client.last().Type != protocol.TypeInvokeNack || len(h.inflight) != 0 {
					t.Error("request crossed settings fence")
				}
				if h.devices["device"].conn != target {
					t.Error("write fence closed transport")
				}
			} else {
				h.Drop(target, protocol.RoleDevice, "device")
			}
			unpause()
			<-done
			if !before {
				stored, err := s.LoadDevice(context.Background(), "device")
				if err != nil {
					t.Fatal(err)
				}
				if h.DeviceOnline("device") || h.devices["device"].ready || stored.SessionEpochActive {
					t.Error("offline target kept a published/active epoch")
				}
			}
			assertRetiredTargetConsent(t, s, active)
		})
	}
}

// Neither approved consent nor a committed rotation may be published active
// if inbound changed while its transaction acknowledgement was delayed.
func TestSessionSettings_shouldRejectStalePublication_whenOffOnFollowsCommit(t *testing.T) {
	for _, operation := range []string{"approve", "resume"} {
		t.Run(operation, func(t *testing.T) {
			s, h, target, original := approvedSessionFixture(t)
			active := original.last()
			client := &recSender{}
			var request protocol.Message
			if operation == "approve" {
				request = nextSessionRequest(t, s, h, client, "pending", 2)
				h.Handle(client, protocol.RoleClient, "device", request)
				request = target.last()
				request.Type = protocol.TypeSessionApprovalResult
				request.Allow = protocol.Bool(true)
			} else {
				h.Drop(original, protocol.RoleClient, "device")
				if code := h.RegisterClient("device", "xr_grant_transport", "", client); code != "" {
					t.Fatal(code)
				}
				request = protocol.Message{Type: protocol.TypeSessionResumeRequest, RequestID: "resume", SessionID: active.SessionID, ResumeID: active.ResumeID, ResumeCapability: active.ResumeCapability, TargetUID: h.devices["device"].record.UID}
			}
			barrier := &resumeCommitBarrier{Store: s.SQLStore, entered: make(chan struct{}), release: make(chan struct{})}
			h.accessCodes = barrier
			var once sync.Once
			unpause := func() { once.Do(func() { close(barrier.release) }) }
			done := make(chan struct{})
			go func() {
				defer close(done)
				if operation == "approve" {
					h.Handle(target, protocol.RoleDevice, "device", request)
				} else {
					h.Handle(client, protocol.RoleClient, "device", request)
				}
			}()
			defer func() { unpause(); <-done }()
			select {
			case <-barrier.entered:
			case <-time.After(3 * time.Second):
				t.Fatal("commit barrier not reached")
			}
			toggleSessionInbound(h, target, false)
			toggleSessionInbound(h, target, true)
			unpause()
			<-done
			if got := client.last(); got.Status != protocol.SessionUnknownState || got.ResumeCapability != "" {
				t.Error("stale commit published active/proof")
			}
			if h.clients[client].session != nil {
				t.Error("stale session rebound")
			}
		})
	}
}
