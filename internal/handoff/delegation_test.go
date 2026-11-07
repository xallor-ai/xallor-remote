package handoff

import (
	"context"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/delegation"
)

// The initial proof must be revalidated before any workspace or CLI process.
// Rejection records an unknown result and releases the never-started job.
func TestManager_should_notStartProcess_whenInitialDelegationIsRejected(t *testing.T) {
	m := newTestManager(context.Background(), t.TempDir())
	defer m.Close()
	m.renew = func(context.Context, string) (time.Duration, error) { return 0, delegation.ErrDenied }
	r := testRequest(t.TempDir())
	if _, err := m.Submit(testScope, r, r.Workspace); err != nil {
		t.Fatal(err)
	}
	m.wg.Wait()
	receipt, err := m.Get(testScope, r.ID)
	if err != nil || receipt.State != "unknown" || receipt.Code != "execution_authorization_unconfirmed" {
		t.Fatal(receipt.State, receipt.Code, err)
	}
	rows, err := m.jobs.Snapshot()
	if err != nil || len(rows) != 0 {
		t.Fatal("unstarted job not released", err)
	}
}
