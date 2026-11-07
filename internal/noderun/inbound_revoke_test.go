package noderun

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/delegation"
	"github.com/xallor-ai/xallor-remote/internal/identity"
	"github.com/xallor-ai/xallor-remote/internal/invocation"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// The acknowledged disabled snapshot cancels existing execution with an
// authorization cause; a later enabled snapshot cannot revive that context.
func TestSyncDeviceContext_should_cancelForegroundExec_whenInboundDisabled(t *testing.T) {
	s, err := identity.LoadFrom(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	d := New(s, nil)
	defer d.Stop()
	defer d.handoffs.Close()
	defer d.jobs.Close()
	ctx, cancel := context.WithCancelCause(d.ctx)
	d.mu.Lock()
	d.local["route"] = cancel
	d.mu.Unlock()
	device := invocation.DeviceContext{
		Authority: strings.Repeat("a", 32),
		DeviceUID: strings.Repeat("b", 32),
		GrantID:   strings.Repeat("c", 32),
		Inbound:   false,
	}
	if err := d.syncDeviceContext(protocol.Message{DeviceContext: &device}); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(context.Cause(ctx), delegation.ErrDenied) {
		t.Fatal(context.Cause(ctx))
	}
	device.Inbound = true
	if err := d.syncDeviceContext(protocol.Message{DeviceContext: &device}); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(context.Cause(ctx), delegation.ErrDenied) {
		t.Fatal("enabling inbound revived the old execution")
	}
	fresh, stop := context.WithCancelCause(d.ctx)
	defer stop(nil)
	d.mu.Lock()
	d.local["fresh"] = stop
	d.mu.Unlock()
	if err := d.syncDeviceContext(protocol.Message{DeviceContext: &device}); err != nil || fresh.Err() != nil {
		t.Fatal("enabled snapshot cancelled new execution", err, fresh.Err())
	}
}
