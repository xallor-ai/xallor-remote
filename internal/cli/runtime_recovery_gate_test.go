package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/filelock"
	"github.com/xallor-ai/xallor-remote/internal/release"
	"github.com/xallor-ai/xallor-remote/internal/runtimeowner"
)

// Missing IPC is not absence of an owner. The actual lifetime lock and durable
// unfinished record each independently refuse recovery before a new admission.
func TestRecoveryRefusesOccupiedServiceOrUnfinishedOperation(t *testing.T) {
	for _, kind := range []string{"service-lock", "unfinished", "corrupt-jobs"} {
		t.Run(kind, func(t *testing.T) {
			profile := t.TempDir()
			t.Setenv("XALLOR_REMOTE_DATA_DIR", profile)
			journal := runtimeowner.Journal{Dir: filepath.Join(profile, "runtime", "lifecycle")}
			var expected error = release.ErrCleanupUncertain
			switch kind {
			case "service-lock":
				unlock, err := filelock.Acquire(filepath.Join(profile, "runtime.lock"))
				if err != nil {
					t.Fatal(err)
				}
				defer unlock()
			case "unfinished":
				tx, err := journal.Admit(runtimeowner.Request{ID: strings.Repeat("1", 32), Kind: "install", IntentSHA256: strings.Repeat("a", 64)})
				if err != nil {
					t.Fatal(err)
				}
				tx.Close()
				expected = runtimeowner.ErrPending
			case "corrupt-jobs":
				if err := os.WriteFile(filepath.Join(profile, "jobs.json"), []byte("{"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := recoverLegacyPending(context.Background(), io.Discard); !errors.Is(err, expected) {
				t.Fatal(kind, err)
			}
			if kind != "unfinished" {
				if _, err := os.Stat(filepath.Join(journal.Dir, "operations.db")); !os.IsNotExist(err) {
					t.Fatal("refusal admitted a new operation", err)
				}
			}
		})
	}
}
