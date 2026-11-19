package runtimeowner

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Both the read-only hint and guarded bootstrap may observe a missing ledger;
// neither initializes an operation database as a side effect of the check.
func TestBootstrap_shouldKeepAbsentJournalAbsent(t *testing.T) {
	j := Journal{Dir: filepath.Join(t.TempDir(), "uninitialized")}
	if err := j.RefuseBootstrap(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(j.Dir); !os.IsNotExist(err) {
		t.Fatalf("read-only gate created profile: %v", err)
	}
	if err := j.WithBootstrap(func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(j.Dir, "operations.db")); !os.IsNotExist(err) {
		t.Fatalf("guard created database: %v", err)
	}
}

// Bootstrap must apply the same durable resource gate as transaction admission,
// even when the recorded operation outcome is already terminal.
func TestBootstrap_shouldRefuseTerminalResourceDebt_beforeRunning(t *testing.T) {
	for _, fault := range []string{"valid_closed", "unknown", "wrong_namespace", "orphan"} {
		t.Run(fault, func(t *testing.T) {
			j := journal(t)
			tx, err := j.Admit(request("1"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = tx.Close() })
			resource := resourceIntentFixture("1")
			if err := tx.RecordResourceIntent(resource); err != nil {
				t.Fatal(err)
			}
			if err := tx.CloseResource(resource.ID, resource, ResourceClosure{Cause: CauseClosed}); err != nil {
				t.Fatal(err)
			}
			if err := tx.Finish(Rejected); err != nil {
				t.Fatal(err)
			}
			switch fault {
			case "unknown":
				_, err = tx.db.Exec("UPDATE resource_obligations SET status=?,cause=?", ResourceUnknown, CauseCleanupUnknown)
			case "wrong_namespace":
				_, err = tx.db.Exec("UPDATE resource_obligations SET namespace_sha256=?", strings.Repeat("f", 64))
			case "orphan":
				_, err = tx.db.Exec("UPDATE resource_obligations SET operation_id=?", request("f").ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := tx.Close(); err != nil {
				t.Fatal(err)
			}
			called := false
			err = j.WithBootstrap(func() error { called = true; return nil })
			if fault == "valid_closed" {
				if err != nil || !called {
					t.Fatalf("valid settled history refused: called=%v err=%v", called, err)
				}
			} else if !errors.Is(err, ErrPending) || called {
				t.Fatalf("bootstrap bypassed %s debt: called=%v err=%v", fault, called, err)
			}
		})
	}
}
