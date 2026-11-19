//go:build linux

package job

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// A FIFO ledger without a writer must refuse before a blocking read. The child
// deadline makes the regression safe even against an implementation that hangs.
func TestCheckReleased_should_refuseFIFO_withoutBlockingShutdown(t *testing.T) {
	dir := t.TempDir()
	if err := unix.Mkfifo(filepath.Join(dir, "jobs.json"), 0600); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, self, "-test.run=^TestJobReleaseFIFOChild$")
	cmd.Env = append(os.Environ(), "XR_JOB_RELEASE_FIFO="+dir)
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil || err != nil {
		t.Fatal("ledger observation blocked or failed", ctx.Err(), err, string(output))
	}
}

// Only invoked by the bounded FIFO observation subprocess.
func TestJobReleaseFIFOChild(t *testing.T) {
	if dir := os.Getenv("XR_JOB_RELEASE_FIFO"); dir != "" {
		if errors.Is(CheckReleased(dir), ErrUnavailable) {
			os.Exit(0)
		}
		os.Exit(1)
	}
}
