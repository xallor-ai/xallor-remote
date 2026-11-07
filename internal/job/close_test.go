package job

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/process"
)

// Hold the exact startup interval used by Owner.Start, after durable intent but
// before process creation. Closing must retain the file lock until that launch
// publishes its result; a new ledger must see debt, not a launch from the past.
func TestLedger_should_retainFileLock_whenCloseRacesWithStartup(t *testing.T) {
	dir := t.TempDir()
	l := New(dir)
	defer l.Close()
	o, err := l.ReserveExec(testScope, "launch")
	if err != nil {
		t.Fatal(err)
	}
	endStart, err := l.beginStart(o.ID())
	if err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	release := func() { once.Do(endStart) }
	defer release()
	closing, closed := make(chan struct{}), make(chan struct{})
	go func() { close(closing); l.Close(); close(closed) }()
	<-closing
	select {
	case <-closed:
		t.Fatal("file lock released before startup completed")
	case <-time.After(100 * time.Millisecond):
	}
	other := New(dir)
	if _, err := other.Snapshot(); !errors.Is(err, ErrUnavailable) {
		t.Fatal("new Runtime acquired ledger during old startup", err)
	}
	other.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	child, err := process.Start(ctx, reservationCommand(dir, o.ID()))
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Wait(); err != nil {
		t.Fatal(err)
	}
	release()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("Close remained blocked after startup released")
	}
	fresh := New(dir)
	defer fresh.Close()
	rows, err := fresh.Snapshot()
	if err != nil || len(rows) != 1 || rows[0].ID != o.ID() || rows[0].State != "uncertain" {
		t.Fatal("close/reopen lost unconfirmed occupancy", rows, err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestOwnerProcessHelper$")
	if child, err := o.Start(ctx, cmd); child != nil || !errors.Is(err, ErrUnavailable) || cmd.Process != nil {
		t.Fatal("old owner launched after file-lock handoff", child, err)
	}
}

// A failed persistence transition must release the startup gate; otherwise
// shutdown deadlocks after rejecting the launch and the next Runtime cannot open.
func TestLedger_should_allowClose_whenStartupPersistenceFails(t *testing.T) {
	l := New(t.TempDir())
	defer l.Close()
	o, err := l.ReserveExec(testScope, "failed-start")
	if err != nil {
		t.Fatal(err)
	}
	l.write = func(string, []byte) error { return errors.New("write failed") }
	cmd := exec.Command(os.Args[0], "-test.run=^TestOwnerProcessHelper$")
	if child, err := o.Start(context.Background(), cmd); child != nil || !errors.Is(err, ErrUnavailable) || cmd.Process != nil {
		t.Fatal("failed persistence permitted launch", child, err)
	}
	done := make(chan struct{})
	go func() { l.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("failed startup retained shutdown gate")
	}
}
