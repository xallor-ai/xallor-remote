package handoff

import (
	"context"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/invocation"
	"github.com/xallor-ai/xallor-remote/internal/job"
)

type testManager struct{ *Manager }

func newTestManager(ctx context.Context, dir string) *testManager {
	m := New(ctx, dir, job.New(dir))
	_ = m.SyncDevice(invocation.DeviceContext{Authority: testScope.Authority, DeviceUID: testScope.DeviceUID, GrantID: testScope.GrantID, Inbound: true})
	return &testManager{m}
}
func (m *testManager) Close() { m.Manager.Close(); m.jobs.Close() }
func (m *testManager) Submit(scope invocation.Scope, r Request, root string) (Receipt, error) {
	return m.Manager.Submit(m.Capture(scope), r, root)
}
func (m *testManager) probeOwner(t *testing.T) *job.Owner {
	t.Helper()
	owner, err := m.jobs.ReserveHandoff(testScope, testSession)
	if err != nil {
		t.Fatal(err)
	}
	return owner
}
