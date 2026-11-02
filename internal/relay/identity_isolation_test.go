package relay

import (
	"context"
	"testing"
	"time"
)

type blockedIdentityStore struct {
	DeviceStore
	entered chan struct{}
	release chan struct{}
}

func (s *blockedIdentityStore) LoadDevice(ctx context.Context, id string) (DeviceRecord, error) {
	if id == "blocked" {
		close(s.entered)
		select {
		case <-s.release:
		case <-ctx.Done():
			return DeviceRecord{}, ctx.Err()
		}
	}
	return s.DeviceStore.LoadDevice(ctx, id)
}

// An unrelated routing lookup must complete while another identity lookup is blocked.
func TestIdentity_should_isolateRouting_whenStoreLookupBlocks(t *testing.T) {
	s := &blockedIdentityStore{DeviceStore: newMemStore(), entered: make(chan struct{}), release: make(chan struct{})}
	h := NewHub(s)
	if code := h.RegisterDevice("healthy", "secret", true, &recSender{}); code != "" {
		t.Fatal(code)
	}
	finished := make(chan struct{})
	go func() { h.RegisterClient("blocked", "grant", &recSender{}); close(finished) }()
	waitSignal(t, s.entered)
	queried := make(chan bool, 1)
	go func() { queried <- h.DeviceOnline("healthy") }()
	select {
	case online := <-queried:
		if !online {
			t.Error("unrelated device unexpectedly offline")
		}
	case <-time.After(time.Second):
		t.Error("database wait blocked unrelated routing state")
	}
	close(s.release)
	<-finished
}
