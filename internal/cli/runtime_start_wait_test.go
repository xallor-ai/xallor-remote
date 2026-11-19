package cli

import (
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/release"
)

// Failed termination must have a bounded wait and retain uncertain outcome.
// The deferred completion also releases the deliberately blocked red-test helper.
func TestStopOwnedRuntime_should_boundWait_whenExitIsUnconfirmed(t *testing.T) {
	done := make(chan runtimeStartResult, 1)
	defer func() { done <- runtimeStartResult{exited: true} }()
	result := make(chan error, 1)
	var calls atomic.Int32
	go func() {
		result <- stopOwnedRuntime(func() error { calls.Add(1); return os.ErrPermission }, done, 5*time.Millisecond)
	}()
	select {
	case err := <-result:
		if !errors.Is(err, release.ErrCleanupUncertain) {
			t.Fatal("unconfirmed exit became success", err)
		}
	case <-time.After(750 * time.Millisecond):
		t.Fatal("owned Runtime cleanup wait exceeded its budget")
	}
	if calls.Load() != 1 {
		t.Fatal("termination was repeated", calls.Load())
	}
}

// A completed Wait owns the evidence; never signal its PID again after exit.
func TestStopOwnedRuntime_should_notSignalAlreadyObservedExit(t *testing.T) {
	done := make(chan runtimeStartResult, 1)
	done <- runtimeStartResult{exited: true}
	calls := 0
	err := stopOwnedRuntime(func() error { calls++; return nil }, done, time.Second)
	if err != nil || calls != 0 {
		t.Fatal("already exited process was signalled", calls, err)
	}
}

// A kill error racing natural exit does not outweigh a confirmed wait/release.
func TestStopOwnedRuntime_should_acceptConfirmedExit_despiteKillError(t *testing.T) {
	done := make(chan runtimeStartResult, 1)
	err := stopOwnedRuntime(func() error {
		done <- runtimeStartResult{exited: true, waitErr: errors.New("nonzero exit")}
		return os.ErrPermission
	}, done, time.Second)
	if err != nil {
		t.Fatal("confirmed exit was lost", err)
	}
}

// Reaping the root without releasing its protected image is still incomplete.
func TestStopOwnedRuntime_should_retainUncertainty_whenWaitOrImageReleaseFails(t *testing.T) {
	for _, result := range []runtimeStartResult{{waitErr: errors.New("wait failed")}, {exited: true, imageErr: errors.New("close failed")}} {
		done := make(chan runtimeStartResult, 1)
		done <- result
		if err := stopOwnedRuntime(func() error { return nil }, done, time.Second); !errors.Is(err, release.ErrCleanupUncertain) {
			t.Fatal("resource uncertainty was hidden", err)
		}
	}
}
