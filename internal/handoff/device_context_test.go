package handoff

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/invocation"
	"github.com/xallor-ai/xallor-remote/internal/job"
)

func testDeviceContext() invocation.DeviceContext {
	return invocation.DeviceContext{Authority: testScope.Authority, DeviceUID: testScope.DeviceUID, GrantID: testScope.GrantID, Inbound: true}
}

// A retained snapshot must survive an ordinary reconnect. A real change
// cancels the worker and blocks late submissions, without releasing its job.
func TestManager_should_cancelOldContext_withoutReleasingUnconfirmedJob(t *testing.T) {
	for _, kind := range []string{"grant", "authority", "device", "claim", "inbound", "invalid"} {
		t.Run(kind, func(t *testing.T) {
			m := newTestManager(context.Background(), t.TempDir())
			defer m.Close()
			owner := m.probeOwner(t)
			ctx, cancel := context.WithCancelCause(context.Background())
			defer cancel(nil)
			m.active[testSession] = worker{scope: testScope, cancel: cancel}
			current := testDeviceContext()
			if err := m.SyncDevice(current); err != nil || ctx.Err() != nil {
				t.Fatal("reconnect interrupted work", err)
			}
			switch kind {
			case "grant":
				current.GrantID = strings.Repeat("d", 32)
			case "authority":
				current.Authority = strings.Repeat("d", 32)
			case "device":
				current.DeviceUID = strings.Repeat("d", 32)
			case "claim":
				current.GrantID, current.OwnershipID = "", strings.Repeat("d", 32)
			case "inbound":
				current.Inbound = false
			case "invalid":
				current = invocation.DeviceContext{}
			}
			err := m.SyncDevice(current)
			if (kind == "invalid") != (err != nil) {
				t.Fatal("context validation", err)
			}
			if !errors.Is(context.Cause(ctx), errDeviceContextChanged) {
				t.Fatal("old worker not cancelled")
			}
			rows, err := m.jobs.Snapshot()
			if err != nil || len(rows) != 1 {
				t.Fatal("cancellation released unconfirmed capacity", rows, err)
			}
			r := testRequest(t.TempDir())
			if _, err := m.Submit(testScope, r, r.Workspace); err == nil || len(m.items) != 0 {
				t.Fatal("late old submit persisted")
			}
			if err := owner.Finish(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// Startup has no authority until the device handshake synchronizes it. A
// caller-provided valid Scope cannot initialize or override that authority.
func TestManager_should_denyBeforeDeviceSynchronization(t *testing.T) {
	dir := t.TempDir()
	ledger := job.New(dir)
	defer ledger.Close()
	m := New(context.Background(), dir, ledger, testRenew)
	defer m.Close()
	r := testRequest(t.TempDir())
	if _, err := m.Submit(m.Capture(testScope, "fixture"), r, r.Workspace); err == nil || len(m.items) != 0 {
		t.Fatal("caller initialized device context")
	}
	if err := m.SyncDevice(testDeviceContext()); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Submit(m.Capture(testScope, "fixture"), r, r.Workspace); err != nil {
		t.Fatal("synchronized context denied", err)
	}
}

// A received invoke can wait behind goroutine scheduling through off/on.
// Returning to the same scope must not resurrect that queued authorization.
func TestManager_should_rejectDelayedTicket_afterInboundDisableAndEnable(t *testing.T) {
	m := newTestManager(context.Background(), t.TempDir())
	defer m.Close()
	ticket := m.Capture(testScope)
	current := testDeviceContext()
	current.Inbound = false
	if err := m.SyncDevice(current); err != nil {
		t.Fatal(err)
	}
	current.Inbound = true
	if err := m.SyncDevice(current); err != nil {
		t.Fatal(err)
	}
	r := testRequest(t.TempDir())
	if _, err := m.Manager.Submit(ticket, r, r.Workspace); err == nil || len(m.items) != 0 {
		t.Fatal("off/on revived a queued invoke")
	}
	fresh := m.Capture(testScope)
	if err := m.SyncDevice(current); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Manager.Submit(fresh, r, r.Workspace); err != nil {
		t.Fatal("idempotent sync invalidated new ticket", err)
	}
}
