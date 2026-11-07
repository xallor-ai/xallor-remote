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

// A real long-running process exercises the Owner rather than a shell parser.
func TestOwnerSequenceHelper(t *testing.T) {
	if os.Getenv("XR_OWNER_SEQUENCE_WAIT") == "1" {
		time.Sleep(time.Minute)
		os.Exit(0)
	}
}

// One reservation must not launch two active supervised process groups. After
// confirmed cleanup it can run the next probe without reserving a second job.
func TestOwner_should_rejectConcurrentStart_and_allowAfterCleanup(t *testing.T) {
	l := New(t.TempDir())
	defer l.Close()
	o, err := l.ReserveHandoff(testScope, "session")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	first := exec.Command(os.Args[0], "-test.run=^TestOwnerSequenceHelper$")
	first.Env = append(os.Environ(), "XR_OWNER_SEQUENCE_WAIT=1")
	child, err := o.Start(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); _ = o.Finish() }()
	second := exec.Command(os.Args[0], "-test.run=^TestOwnerProcessHelper$")
	other, err := o.Start(ctx, second)
	if other != nil || !errors.Is(err, ErrActive) || second.Process != nil {
		t.Fatal("one reservation launched a second process before cleanup")
	}
	if err := child.Stop(); err != nil {
		t.Fatal(err)
	}
	_ = child.Wait()
	third := exec.Command(os.Args[0], "-test.run=^TestOwnerProcessHelper$")
	next, err := o.Start(ctx, third)
	if err != nil {
		t.Fatal("confirmed cleanup blocked next probe", err)
	}
	if err := next.Wait(); err != nil {
		t.Fatal(err)
	}
	if err := o.Finish(); err != nil {
		t.Fatal(err)
	}
	rows, err := l.Snapshot()
	if err != nil || len(rows) != 0 {
		t.Fatal(rows, err)
	}
}

type settledProcess struct {
	done chan struct{}
	err  error
}

func (p settledProcess) Done() <-chan struct{} { return p.done }
func (p settledProcess) Wait() error           { <-p.done; return p.err }

// A completed wait with unconfirmed cleanup is still resource debt. The next
// Start must not launch a probe, and Finish must persist that uncertainty.
func TestOwner_should_blockNextLaunch_whenPreviousCleanupUnconfirmed(t *testing.T) {
	l := New(t.TempDir())
	defer l.Close()
	o, err := l.ReserveHandoff(testScope, "session")
	if err != nil {
		t.Fatal(err)
	}
	if err := l.prepareStart(o.ID()); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	close(done)
	o.child = settledProcess{done: done, err: process.ErrCleanup}
	cmd := exec.Command(os.Args[0], "-test.run=^TestOwnerProcessHelper$")
	if child, err := o.Start(context.Background(), cmd); child != nil || !errors.Is(err, ErrUncertain) || cmd.Process != nil {
		t.Fatal("unconfirmed cleanup permitted new launch", err)
	}
	if !errors.Is(o.Finish(), ErrUncertain) {
		t.Fatal("lost resource debt")
	}
	rows, err := l.Snapshot()
	if err != nil || len(rows) != 1 || rows[0].State != "uncertain" {
		t.Fatal(rows, err)
	}
}

// Simultaneous callers sharing one owner get one process, not one process per
// goroutine. The losing commands must never reach OS process creation.
func TestOwner_should_launchOneProcess_whenStartsRace(t *testing.T) {
	l := New(t.TempDir())
	defer l.Close()
	o, err := l.ReserveExec(testScope, "race")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer func() { cancel(); _ = o.Finish() }()
	gate := make(chan struct{})
	var wg sync.WaitGroup
	winners := make(chan bool, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := exec.Command(os.Args[0], "-test.run=^TestOwnerSequenceHelper$")
			cmd.Env = append(os.Environ(), "XR_OWNER_SEQUENCE_WAIT=1")
			<-gate
			child, err := o.Start(ctx, cmd)
			if err == nil {
				winners <- true
				return
			}
			if !errors.Is(err, ErrActive) || child != nil || cmd.Process != nil {
				t.Errorf("unexpected loser: %v", err)
			}
		}()
	}
	close(gate)
	wg.Wait()
	if len(winners) != 1 {
		t.Fatalf("launched %d processes", len(winners))
	}
}
