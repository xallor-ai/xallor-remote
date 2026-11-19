package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/filelock"
	"github.com/xallor-ai/xallor-remote/internal/invocation"
	"github.com/xallor-ai/xallor-remote/internal/job"
	"github.com/xallor-ai/xallor-remote/internal/release"
)

// A dead Runtime does not prove its jobs were released. Stop must not report
// success for occupied/uncertain/corrupt records or silently rewrite that debt.
func TestStop_should_preserveUnreleasedJobEvidence_whenRuntimeIsAbsent(t *testing.T) {
	for _, state := range []string{"occupied", "uncertain", "corrupt", "reserved"} {
		t.Run(state, func(t *testing.T) {
			profile := t.TempDir()
			t.Setenv("XALLOR_REMOTE_DATA_DIR", profile)
			id := strings.Repeat("d", 32)
			entry := job.Record{ID: id, Kind: "exec", Key: "owned-execution", State: state,
				Scope: invocation.Scope{Authority: strings.Repeat("a", 32), DeviceUID: strings.Repeat("b", 32), GrantID: strings.Repeat("c", 32)}}
			raw, err := json.Marshal(map[string]any{"version": 1, "jobs": map[string]job.Record{id: entry}})
			if err != nil {
				t.Fatal(err)
			}
			if state == "corrupt" {
				raw = []byte(`{"version":1,`)
			}
			path := filepath.Join(profile, "jobs.json")
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			err = (runtimeControl{}).Stop()
			if state == "reserved" {
				if err != nil {
					t.Fatal("never-started reservation treated as a running process", err)
				}
			} else if !errors.Is(err, release.ErrCleanupUncertain) {
				t.Fatal("unconfirmed task cleanup was reported stopped", err)
			}
			if state != "reserved" && err.Error() != "runtime cleanup is uncertain：任务资源释放尚未确认，请保留现有记录并检查任务进程。" {
				t.Fatalf("task release refusal lost its specific user diagnostic: %v", err)
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != string(raw) {
				t.Fatal("stop rewrote job evidence", err)
			}
		})
	}
}

// Another ledger owner may still be launching or releasing a child even when
// the Runtime pipe is absent. No lock contention may be called clean shutdown.
func TestStop_should_refuseConfirmation_whenJobLedgerIsStillOwned(t *testing.T) {
	profile := t.TempDir()
	t.Setenv("XALLOR_REMOTE_DATA_DIR", profile)
	unlock, err := filelock.Acquire(filepath.Join(profile, "jobs.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if err := (runtimeControl{}).Stop(); !errors.Is(err, release.ErrCleanupUncertain) {
		t.Fatal("live ledger owner ignored", err)
	}
}
