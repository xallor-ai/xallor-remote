package job

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"
)

// The helper is a real child process so occupancy follows process cleanup,
// independently of shell parsing, transport or a successful business result.
func TestOwnerProcessHelper(t *testing.T) {
	if os.Getenv("XR_JOB_OWNER_HELPER") == "1" {
		os.Exit(7)
	}
}

// A nonzero result still frees confirmed resources, but only when the owning
// job finishes. Reading/repeating the process result cannot release twice.
func TestOwner_should_releaseAfterConfirmedNonzeroExit(t *testing.T) {
	l := New(t.TempDir())
	defer l.Close()
	o, err := l.Reserve(testScope, "exec", "one")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.Command(os.Args[0], "-test.run=^TestOwnerProcessHelper$")
	cmd.Env = append(os.Environ(), "XR_JOB_OWNER_HELPER=1")
	c, err := o.Start(ctx, cmd)
	if err != nil {
		t.Fatal(err)
	}
	var exit *exec.ExitError
	if err := c.Wait(); !errors.As(err, &exit) || exit.ExitCode() != 7 {
		t.Fatal(err)
	}
	rows, err := l.Snapshot()
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
	if o.Finish() != nil || o.Finish() != nil {
		t.Fatal("release failed")
	}
	rows, err = l.Snapshot()
	if err != nil || len(rows) != 0 {
		t.Fatal(rows, err)
	}
	if _, err := o.Start(ctx, cmd); !errors.Is(err, ErrUnavailable) {
		t.Fatal("finished owner relaunched")
	}
}

// Cancellation before process start proves no process exists and permits the
// reservation to be released, unlike recovery of an interrupted Runtime.
func TestOwner_should_releaseReservation_whenCancelledBeforeStart(t *testing.T) {
	l := New(t.TempDir())
	defer l.Close()
	o, err := l.Reserve(testScope, "exec", "one")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cmd := exec.Command(os.Args[0])
	if _, err := o.Start(ctx, cmd); !errors.Is(err, context.Canceled) || cmd.Process != nil {
		t.Fatal(err)
	}
	if o.Finish() != nil {
		t.Fatal("unstarted reservation leaked")
	}
	rows, err := l.Snapshot()
	if err != nil || len(rows) != 0 {
		t.Fatal(rows, err)
	}
}

// A lost release write cannot report success or admit another job in the same
// Runtime. On reopen, only the persisted outcome determines retained debt.
func TestOwner_should_failClosed_whenReleasePersistenceFails(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(strconv.FormatBool(committed), func(t *testing.T) {
			dir := t.TempDir()
			l := New(dir)
			defer l.Close()
			o, err := l.Reserve(testScope, "exec", "one")
			if err != nil {
				t.Fatal(err)
			}
			if err := l.prepareStart(o.ID()); err != nil {
				t.Fatal(err)
			}
			write := l.write
			l.write = func(path string, b []byte) error {
				if committed {
					if err := write(path, b); err != nil {
						return err
					}
				}
				return errors.New("lost release acknowledgement")
			}
			if !errors.Is(o.Finish(), ErrUnavailable) || !errors.Is(o.Finish(), ErrUnavailable) {
				t.Fatal("unconfirmed release reported success")
			}
			l.write = write
			if _, err := l.Reserve(testScope, "exec", "two"); !errors.Is(err, ErrUnavailable) {
				t.Fatal("unconfirmed storage state admitted work", err)
			}
			l.Close()
			reopened := New(dir)
			defer reopened.Close()
			rows, err := reopened.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			if committed && len(rows) != 0 {
				t.Fatal("confirmed cleanup with persisted release was resurrected", rows)
			}
			if !committed && (len(rows) != 1 || rows[0].State != "uncertain") {
				t.Fatal("unpersisted release lost occupancy", rows)
			}
		})
	}
}

// Closing the shared ledger invalidates an unused launch capability without
// erasing its reservation in place; a new Runtime can recover this never-started
// reservation, but the old Owner must remain invalid after the lock is released.
func TestOwner_should_refuseStart_whenLedgerClosed(t *testing.T) {
	dir := t.TempDir()
	l := New(dir)
	o, err := l.Reserve(testScope, "exec", "one")
	if err != nil {
		t.Fatal(err)
	}
	l.Close()
	cmd := exec.Command(os.Args[0], "-test.run=^TestOwnerProcessHelper$")
	cmd.Env = append(os.Environ(), "XR_JOB_OWNER_HELPER=1")
	if _, err := o.Start(context.Background(), cmd); !errors.Is(err, ErrUnavailable) || cmd.Process != nil {
		t.Fatal("closed ledger permitted launch", err)
	}
	if !errors.Is(o.Finish(), ErrUnavailable) {
		t.Fatal("closed ledger permitted release")
	}
	reopened := New(dir)
	defer reopened.Close()
	rows, err := reopened.Snapshot()
	if err != nil || len(rows) != 0 {
		t.Fatal(rows, err)
	}
}
