package handoff

import (
	"context"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/job"
)

type testManager struct{ *Manager }

func newTestManager(ctx context.Context, dir string) *testManager {
	return &testManager{New(ctx, dir, job.New(dir))}
}
func (m *testManager) Close() { m.Manager.Close(); m.jobs.Close() }
func (m *testManager) probeOwner(t *testing.T) *job.Owner {
	t.Helper()
	owner, err := m.jobs.Reserve(testScope, "handoff", testSession)
	if err != nil {
		t.Fatal(err)
	}
	return owner
}
