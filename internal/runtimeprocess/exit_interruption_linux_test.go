//go:build linux

package runtimeprocess

import (
	"os"
	"runtime"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// A live process must remain observable while ordinary Go preemption signals
// interrupt the observing thread; a signal is not evidence of process exit.
func TestWatchExit_should_observeLiveProcess_whenThreadReceivesSignals(t *testing.T) {
	watch, err := WatchExit(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	defer watch.Close()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	tid := unix.Gettid()
	stop := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		for {
			select {
			case <-stop:
				done <- nil
				return
			default:
			}
			if err := unix.Tgkill(os.Getpid(), tid, unix.SIGURG); err != nil {
				done <- err
				return
			}
			runtime.Gosched()
		}
	}()
	defer func() {
		close(stop)
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	deadline := time.Now().Add(2 * time.Second)
	checks := 0
	for time.Now().Before(deadline) {
		exited, err := watch.Exited()
		checks++
		if err != nil || exited {
			t.Fatalf("live process snapshot after %d checks: exited=%v err=%v", checks, exited, err)
		}
	}
	t.Logf("live process snapshots under real SIGURG interruption: %d", checks)
}
