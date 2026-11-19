package cli

import (
	"context"
	"github.com/xallor-ai/xallor-remote/internal/release"
	"path/filepath"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/filelock"
)

type recoveryBoundaryControl struct {
	runtimeControl
	start func() error
}

func (c recoveryBoundaryControl) Start(string, string) error { return c.start() }

// After confirmed absence, recovery keeps the same service mutex through state
// revocation. Replacement startup is the only point that releases it early.
func TestRecoveryControlHoldsServiceMutexThroughRevocation(t *testing.T) {
	for _, startReplacement := range []bool{false, true} {
		profile := t.TempDir()
		t.Setenv("XALLOR_REMOTE_DATA_DIR", profile)
		path := filepath.Join(profile, "runtime.lock")
		started := false
		base := recoveryBoundaryControl{start: func() error {
			started = true
			unlock, err := filelock.Acquire(path)
			if err != nil {
				t.Fatal("replacement started while recovery held service mutex", err)
			}
			unlock()
			return nil
		}}
		c := &recoveryControl{Control: base, profile: profile}
		attempt, err := c.PrepareStop(context.Background(), release.StopRequest{Before: release.ObserveSelection(release.State{}), AbsencePolicy: "explicit_legacy_recovery"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := attempt.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
		if unlock, err := filelock.Acquire(path); err == nil {
			unlock()
			t.Fatal("revocation interval did not retain service mutex")
		}
		if startReplacement {
			if err := c.Start("unused", "unused"); err != nil {
				t.Fatal(err)
			}
		} else {
			c.Close()
		}
		if started != startReplacement {
			t.Fatal("unexpected replacement launch")
		}
		unlock, err := filelock.Acquire(path)
		if err != nil {
			t.Fatal("recovery did not release service mutex", err)
		}
		unlock()
		c.Close()
	}
}
