package relay

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

type contextSender struct {
	recSender
	ctx context.Context
}

func (s *contextSender) Context() context.Context { return s.ctx }

func waitCondition(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("fixture condition not reached")
		}
		time.Sleep(time.Millisecond)
	}
}

// Cancelled same-key waiters must release references; another key is independent.
func TestIdentityGate_should_reclaimEntries_whenWaiterCancelled(t *testing.T) {
	var gates identityGates
	unlock, err := gates.acquire(context.Background(), "held")
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	other, err := gates.acquire(context.Background(), "other")
	if err != nil {
		t.Fatal(err)
	}
	other()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		release, err := gates.acquire(ctx, "held")
		if release != nil {
			release()
		}
		result <- err
	}()
	waitCondition(t, func() bool { gates.mu.Lock(); defer gates.mu.Unlock(); return gates.entries["held"].users == 2 })
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	unlock()
	unlock() // release is idempotent
	gates.mu.Lock()
	defer gates.mu.Unlock()
	if len(gates.entries) != 0 {
		t.Fatal("gate entries leaked")
	}
}

// Disconnect cancellation must reach an in-progress store lookup and leave no client binding or gate.
func TestIdentity_should_cancelLookup_whenConnectionCancelled(t *testing.T) {
	s := &blockedIdentityStore{DeviceStore: newMemStore(), entered: make(chan struct{}), release: make(chan struct{})}
	h := NewHub(s, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := &contextSender{ctx: ctx}
	result := make(chan string, 1)
	go func() { result <- h.RegisterClient("blocked", "grant", "", client) }()
	waitSignal(t, s.entered)
	cancel()
	select {
	case code := <-result:
		if code != protocol.RelayError {
			t.Fatal(code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("store did not receive cancellation")
	}
	if len(h.clients) != 0 || len(h.identities.entries) != 0 {
		t.Fatal("cancelled registration retained state")
	}
}

type rotatingStore struct {
	DeviceStore
	entered, release chan struct{}
}

func (s *rotatingStore) UpdateDevice(ctx context.Context, id string, record access.Device) (access.Device, error) {
	if record.GrantHash == protocol.SHA256Hex("rotated") {
		close(s.entered)
		select {
		case <-s.release:
		case <-ctx.Done():
			return access.Device{}, ctx.Err()
		}
	}
	return s.DeviceStore.UpdateDevice(ctx, id, record)
}

// Drop may race with a durable rotation; a replacement and client authentication
// must observe the committed grant, and the old device connection cannot revoke it.
func TestIdentity_should_serializeReplacementWithRotation_whenDeviceDrops(t *testing.T) {
	s := &rotatingStore{DeviceStore: newMemStore(), entered: make(chan struct{}), release: make(chan struct{})}
	h := NewHub(s, nil, nil)
	old, replacement := &recSender{}, &recSender{}
	if code := h.RegisterDevice("device", "secret", true, old); code != "" {
		t.Fatal(code)
	}
	h.Handle(old, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeDeviceSettings, RequestID: "settings", Inbound: protocol.Bool(true), GrantHash: protocol.SHA256Hex("original")})
	finished := make(chan struct{})
	go func() {
		h.Handle(old, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeDeviceSettings, RequestID: "settings", Inbound: protocol.Bool(true), GrantHash: protocol.SHA256Hex("rotated")})
		close(finished)
	}()
	waitSignal(t, s.entered)
	h.Drop(old, protocol.RoleDevice, "device")
	registered := make(chan string, 1)
	go func() { registered <- h.RegisterDevice("device", "secret", true, replacement) }()
	authenticated := make(chan string, 1)
	go func() { authenticated <- h.RegisterClient("device", "original", "", &recSender{}) }()
	waitCondition(t, func() bool {
		h.identities.mu.Lock()
		defer h.identities.mu.Unlock()
		return h.identities.entries["device"].users == 3
	})
	close(s.release)
	waitSignal(t, finished)
	if code := <-registered; code != "" {
		t.Fatal(code)
	}
	if code := <-authenticated; code != protocol.Unauthorized {
		t.Fatalf("accepted stale grant: %s", code)
	}
	h.Handle(old, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeRevoke})
	if !h.DeviceOnline("device") {
		t.Fatal("old device revoked replacement")
	}
	if code := h.RegisterClient("device", "rotated", "", &recSender{}); code != "" {
		t.Fatal(code)
	}
}

// Every identity operation must respect context cancellation while waiting for a SQL connection.
func TestSQLIdentity_should_cancelPoolWait_withoutMutatingRows(t *testing.T) {
	s := openIdentityTestStore(t)
	for name, call := range map[string]func(context.Context) error{
		"register": func(ctx context.Context) error { _, err := s.RegisterDevice(ctx, "device", "secret", true); return err },
		"load":     func(ctx context.Context) error { _, err := s.LoadDevice(ctx, "device"); return err },
		"update":   func(ctx context.Context) error { _, err := s.UpdateDevice(ctx, "device", access.Device{}); return err },
		"delete":   func(ctx context.Context) error { return s.DeleteDevice(ctx, "device", "uid", "secret") },
	} {
		t.Run(name, func(t *testing.T) {
			held, err := s.db.Conn(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer held.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			before := s.db.Stats().WaitCount
			result := make(chan error, 1)
			go func() { result <- call(ctx) }()
			waitCondition(t, func() bool { return s.db.Stats().WaitCount > before })
			cancel()
			select {
			case err := <-result:
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("SQL pool wait ignored cancellation")
			}
		})
	}
	if _, err := s.LoadDevice(context.Background(), "device"); !errors.Is(err, ErrDeviceNotFound) {
		t.Fatalf("cancelled mutation changed state: %v", err)
	}
}
