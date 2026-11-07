package relay

import (
	"context"
	"sync"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/admission"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

type observedContext struct {
	context.Context
	observed chan struct{}
	once     sync.Once
}

func (c *observedContext) Err() error {
	err := c.Context.Err()
	c.once.Do(func() { close(c.observed) })
	return err
}

// Cancellation after the entry check but before routing admission must not start a queued invoke.
func TestIdentity_should_recheckConnection_whenInvokeWaitsForRoutingLock(t *testing.T) {
	h := NewHub(nil, nil, nil, nil, admission.New(admission.Limits{PerDevice: protocol.MaxDeviceRequests}))
	device := &recSender{}
	client := &contextSender{ctx: context.Background()}
	if code := h.RegisterDevice("dev_a", "secret", true, device); code != "" {
		t.Fatal(code)
	}
	authorizeTestClient(t, h, device, client)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	observed := &observedContext{Context: ctx, observed: make(chan struct{})}
	client.ctx = observed
	h.mu.Lock()
	finished := make(chan struct{})
	go func() {
		h.Handle(client, protocol.RoleClient, "dev_a", protocol.Message{Type: protocol.TypeInvoke, ExecID: "after-close", Op: protocol.OpExec})
		close(finished)
	}()
	<-observed.observed
	cancel()
	h.mu.Unlock()
	waitSignal(t, finished)
	if len(h.requests) != 0 || len(h.inflight) != 0 {
		t.Fatal("disconnected client started an execution")
	}
	if h.admission.Active(h.devices["dev_a"].record.UID) != 0 {
		t.Fatal("cancelled admission leaked transport capacity")
	}
}
