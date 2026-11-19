package cli

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// Commit takes precedence over head exit, including a commit between the first
// state read and parent observation. Unknown parent state cannot prolong pending.
func TestLaunchCommit_should_allowOnlySelectedOrLiveBoundedPending(t *testing.T) {
	for _, tc := range []struct {
		name                                                           string
		first, second, gone, expired, watchError, wantReady, wantError bool
	}{
		{name: "pending"},
		{name: "gone", gone: true, wantError: true},
		{name: "expired", expired: true, wantError: true},
		{name: "watch-error", watchError: true, wantError: true},
		{name: "selected-parent-gone", first: true, gone: true, wantReady: true},
		{name: "commit-races-parent-exit", second: true, gone: true, wantReady: true},
		{name: "commit-races-deadline", second: true, expired: true, wantReady: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reads, watches := 0, 0
			selected := func() (bool, error) {
				reads++
				if reads == 1 {
					return tc.first, nil
				}
				return tc.second, nil
			}
			exited := func() (bool, error) {
				watches++
				if tc.watchError {
					return false, errors.New("observer failed")
				}
				return tc.gone, nil
			}
			deadline := time.Now().Add(time.Hour)
			if tc.expired {
				deadline = time.Now().Add(-time.Second)
			}
			ready, err := checkLaunchCommit(selected, exited, deadline)
			if ready != tc.wantReady || (err != nil) != tc.wantError {
				t.Fatal(ready, err)
			}
			if tc.first && watches != 0 {
				t.Fatal("committed owner still depends on head")
			}
		})
	}
}

// Shutdown waits for an in-flight gate read before releasing its OS observer.
// A late IPC/polling call is rejected and cannot touch a recycled descriptor.
func TestLaunchCommitGate_should_serializeShutdownWithPendingObservation(t *testing.T) {
	entered, releaseRead := make(chan struct{}), make(chan struct{})
	var polls, closes atomic.Int32
	gate := &launchCommitGate{
		selected: func() (bool, error) { close(entered); <-releaseRead; return false, nil },
		exited: func() (bool, error) {
			polls.Add(1)
			if closes.Load() != 0 {
				t.Error("polled closed observer")
			}
			return false, nil
		},
		closeObserver: func() error { closes.Add(1); return nil },
		deadline:      time.Now().Add(time.Hour),
	}
	checked := make(chan struct{})
	go func() { defer close(checked); _, _ = gate.Check() }()
	<-entered
	closing, closed := make(chan struct{}), make(chan struct{})
	go func() { close(closing); _ = gate.Close(); close(closed) }()
	<-closing
	select {
	case <-closed:
		t.Error("observer closed during pending gate")
	case <-time.After(20 * time.Millisecond):
	}
	close(releaseRead)
	<-checked
	<-closed
	if polls.Load() != 1 || closes.Load() != 1 {
		t.Fatal("unexpected observer lifetime", polls.Load(), closes.Load())
	}
	if _, err := gate.Check(); err == nil {
		t.Fatal("late gate accepted")
	}
	_ = gate.Close()
	if polls.Load() != 1 || closes.Load() != 1 {
		t.Fatal("closed observer reused")
	}
}
