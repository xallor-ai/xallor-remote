package job

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/process"
)

// Backend rejection must happen before persisting a started occupancy or
// creating an OS process; the unused reservation can still finish cleanly.
func TestOwner_shouldRejectUnavailableBackendBeforeLaunchBoundary(t *testing.T) {
	l := New(t.TempDir())
	defer l.Close()
	o, err := l.ReserveExec(testScope, "backend-refusal")
	if err != nil {
		t.Fatal(err)
	}
	before, err := l.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"low_privilege", "cgroup", "container", "unknown"} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestOwnerProcessHelper$")
		child, err := o.StartWithBackend(context.Background(), cmd, name)
		if child != nil || cmd.Process != nil || !errors.Is(err, process.ErrBackendUnavailable) {
			t.Fatalf("unsupported backend launched: %s %v", name, err)
		}
		after, err := l.Snapshot()
		if err != nil || len(after) != 1 || after[0] != before[0] {
			t.Fatal("rejection crossed persisted launch boundary", after, err)
		}
	}
	if err := o.Finish(); err != nil {
		t.Fatal(err)
	}
	if rows, err := l.Snapshot(); err != nil || len(rows) != 0 {
		t.Fatal("unstarted backend refusal leaked capacity", rows, err)
	}
}
