//go:build windows

package process

import (
	"bytes"
	"errors"
	"os/exec"
	"testing"
	"time"
)

// Deliberately invalidate an adapter-owned standard file, reproducing a native
// Close failure without leaving a live child. The first result must survive.
func TestCommandIO_shouldPreserveOwnedCloseFailure(t *testing.T) {
	s, err := prepareCommandIO(exec.Command("unused"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.files[0].Close(); err != nil {
		t.Fatal(err)
	}
	err = s.finish(time.Second)
	if !errors.Is(err, ErrCleanup) {
		t.Fatal("owned file close failure discarded", err)
	}
}

// Closing after drained copy completion must not close the same file twice and
// invent uncertainty; real close failures are separately retained above.
func TestCommandIO_shouldCloseExactlyOnce_afterCopyCompletion(t *testing.T) {
	var output bytes.Buffer
	cmd := exec.Command("unused")
	cmd.Stdout = &output
	s, err := prepareCommandIO(cmd)
	if err != nil {
		t.Fatal(err)
	}
	s.startCopies()
	if err := s.finish(time.Second); err != nil {
		t.Fatal(err)
	}
}

// The child-end close occurs before the suspended process is resumed. A native
// failure here must stop startup and remain visible during later cleanup.
func TestCommandIO_shouldPreserveCloseFailure_whenStartingCopies(t *testing.T) {
	s, err := prepareCommandIO(exec.Command("unused"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.files[1].Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.startCopies(); !errors.Is(err, ErrCleanup) {
		t.Fatal("startup close discarded", err)
	}
	first := s.closeAll()
	again := s.closeAll()
	if first == nil || again == nil || first.Error() != again.Error() {
		t.Fatal("first close failure not retained", first, again)
	}
}

// A pre-invalidated owned reader fails inside the copy goroutine; finish must
// join it and its Close failure, rather than reporting successful drain.
func TestCommandIO_shouldPreserveCloseFailure_fromCopyGoroutine(t *testing.T) {
	var output bytes.Buffer
	cmd := exec.Command("unused")
	cmd.Stdout = &output
	s, err := prepareCommandIO(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.parent[0].file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.startCopies(); err != nil {
		t.Fatal(err)
	}
	if err := s.finish(time.Second); !errors.Is(err, ErrCleanup) {
		t.Fatal("copy close discarded", err)
	}
}

// Zero is permanently invalid, so this exercises the native close error without
// risking a stale handle being recycled into an unrelated kernel object.
func TestJob_shouldPreserveCleanupFailure_whenThreadCloseFails(t *testing.T) {
	j := &job{}
	err := j.releaseThread()
	if !errors.Is(err, ErrCleanup) || j.thread != 0 {
		t.Fatal("thread cleanup uncertainty lost", err, j.thread)
	}
}
