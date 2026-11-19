package runtimeowner

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// The child exits at the effect boundary without defers: the kernel must release
// its writer lock while the durable intent remains unfinished, not replayable.
func TestProcessExitLeavesDurableEffectAndBlocksNewTransaction(t *testing.T) {
	j := journal(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestJournalProcessHelper$")
	cmd.Env = append(os.Environ(), "XR_TEST_OWNER_JOURNAL="+j.Dir)
	hideTestProcess(cmd)
	output, err := cmd.CombinedOutput()
	var exited *exec.ExitError
	if !errors.As(err, &exited) || exited.ExitCode() != 73 {
		t.Fatalf("child did not reach effect boundary: %v %s", err, output)
	}
	got, err := j.Query(request("1").ID)
	if err != nil || len(got.Effects) != 3 || got.Effects[2].State != EffectPending {
		t.Fatal(got, err)
	}
	if _, err := j.Admit(request("2")); !errors.Is(err, ErrPending) {
		t.Fatal("new transaction admitted after process exit", err)
	}
	if _, err := j.Admit(request("1")); !errors.Is(err, ErrRecorded) {
		t.Fatal("old effect replayed", err)
	}
}

// Helper mode exists only in this package's test executable.
func TestJournalProcessHelper(t *testing.T) {
	dir := os.Getenv("XR_TEST_OWNER_JOURNAL")
	if dir == "" {
		t.Skip("subprocess helper")
	}
	tx, err := (Journal{Dir: dir}).Admit(request("1"))
	if err != nil {
		t.Fatal(err)
	}
	bindFixture(t, tx, boundIntent("prepare", strings.Repeat("b", 64)).Before, strings.Repeat("b", 64))
	prefixFixture(t, tx, "start")
	_ = testEffect(tx, "start", strings.Repeat("b", 64), func() (EffectState, error) { os.Exit(73); return EffectUnknown, nil })
	t.Fatal("effect callback unexpectedly returned")
}
