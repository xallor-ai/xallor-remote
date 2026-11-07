package job

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

// No Start was called before the owner disappeared. Recovery can prove that
// this reservation never launched a process and must return its capacity.
func TestLedger_should_releaseUnstartedReservations_whenReopened(t *testing.T) {
	dir := t.TempDir()
	l := New(dir)
	if _, err := l.ReserveExec(testScope, "never-started"); err != nil {
		t.Fatal(err)
	}
	l.Close()
	reopened := New(dir)
	defer reopened.Close()
	rows, err := reopened.Snapshot()
	if err != nil || len(rows) != 0 {
		t.Fatalf("unstarted reservation retained after restart: rows=%v err=%v", rows, err)
	}
}

// This child observes the ledger before doing any work. Its marker proves
// that execution began only after its own occupied record was published.
func TestReservationProcessHelper(t *testing.T) {
	dir := os.Getenv("XR_RESERVATION_HELPER")
	if dir == "" {
		return
	}
	b, err := os.ReadFile(filepath.Join(dir, "jobs.json"))
	var disk diskState
	if err != nil || json.Unmarshal(b, &disk) != nil || disk.Jobs[os.Getenv("XR_RESERVATION_ID")].State != "occupied" {
		os.Exit(41)
	}
	if os.WriteFile(filepath.Join(dir, "started"), []byte("occupied"), 0600) != nil {
		os.Exit(42)
	}
	os.Exit(0)
}

func reservationCommand(dir, id string) *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=^TestReservationProcessHelper$")
	cmd.Env = append(os.Environ(), "XR_RESERVATION_HELPER="+dir, "XR_RESERVATION_ID="+id)
	return cmd
}

// A real child must see occupied on disk. Even after it exits, losing the
// Owner before Finish leaves debt because cleanup was not durably acknowledged.
func TestOwner_should_persistLaunchBeforeChild_and_retainUnfinishedOccupancy(t *testing.T) {
	dir := t.TempDir()
	l := New(dir)
	defer l.Close()
	o, err := l.ReserveExec(testScope, "actual-child")
	if err != nil {
		t.Fatal(err)
	}
	child, err := o.Start(context.Background(), reservationCommand(dir, o.ID()))
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Wait(); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "started")); err != nil || string(b) != "occupied" {
		t.Fatal(string(b), err)
	}
	l.Close()
	reopened := New(dir)
	defer reopened.Close()
	rows, err := reopened.Snapshot()
	if err != nil || len(rows) != 1 || rows[0].State != "uncertain" {
		t.Fatal(rows, err)
	}
}

// Whether a failed launch-marker write committed or not, Start must not run
// the child. Recovery uses the durable state, never the failed caller's guess.
func TestOwner_should_notStartChild_whenLaunchPersistenceFails(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(strconv.FormatBool(committed), func(t *testing.T) {
			dir := t.TempDir()
			l := New(dir)
			defer l.Close()
			o, err := l.ReserveExec(testScope, "blocked-child")
			if err != nil {
				t.Fatal(err)
			}
			write := l.write
			l.write = func(path string, data []byte) error {
				if committed {
					if err := write(path, data); err != nil {
						return err
					}
				}
				return errors.New("launch write result lost")
			}
			cmd := reservationCommand(dir, o.ID())
			if child, err := o.Start(context.Background(), cmd); child != nil || !errors.Is(err, ErrUnavailable) || cmd.Process != nil {
				t.Fatal(child, err, cmd.Process)
			}
			if _, err := os.Stat(filepath.Join(dir, "started")); !os.IsNotExist(err) {
				t.Fatal("child executed despite failed persistence", err)
			}
			if !errors.Is(o.Finish(), ErrUnavailable) {
				t.Fatal("failed ledger released occupancy")
			}
			l.Close()
			reopened := New(dir)
			defer reopened.Close()
			rows, err := reopened.Snapshot()
			if err != nil || committed && (len(rows) != 1 || rows[0].State != "uncertain") || !committed && len(rows) != 0 {
				t.Fatal(rows, err)
			}
		})
	}
}

// Recovery may erase only reserved records. Existing occupied/uncertain jobs
// keep their identity and exclusion, even alongside reclaimable reservations.
func TestLedger_should_recoverOnlyReservations_whenStatesAreMixed(t *testing.T) {
	dir := t.TempDir()
	l := New(dir)
	defer l.Close()
	for _, state := range []string{"reserved", "occupied", "uncertain"} {
		o, err := l.ReserveExec(testScope, state)
		if err != nil {
			t.Fatal(err)
		}
		if state != "reserved" {
			if err := l.prepareStart(o.ID()); err != nil {
				t.Fatal(err)
			}
		}
		if state == "uncertain" {
			o.uncertain = true
			if !errors.Is(o.Finish(), ErrUncertain) {
				t.Fatal("uncertain owner released")
			}
		}
	}
	l.Close()
	reopened := New(dir)
	defer reopened.Close()
	rows, err := reopened.Snapshot()
	if err != nil || len(rows) != 2 {
		t.Fatal(rows, err)
	}
	for _, r := range rows {
		if r.Key == "reserved" || r.State != "uncertain" {
			t.Fatal(r)
		}
		if _, err := reopened.ReserveExec(testScope, r.Key); !errors.Is(err, ErrUncertain) {
			t.Fatal("unconfirmed operation reused", err)
		}
	}
}

// Publishing recovered state may fail before or after replacement. This
// instance must stay unavailable; only a fresh lock owner may inspect it again.
func TestLedger_should_refuseAdmission_whenRecoveryWriteFails(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(strconv.FormatBool(committed), func(t *testing.T) {
			dir := t.TempDir()
			l := New(dir)
			if _, err := l.ReserveExec(testScope, "reservation"); err != nil {
				t.Fatal(err)
			}
			l.Close()
			reopened := New(dir)
			defer reopened.Close()
			write := reopened.write
			reopened.write = func(path string, data []byte) error {
				if committed {
					if err := write(path, data); err != nil {
						return err
					}
				}
				return errors.New("recovery write result lost")
			}
			if _, err := reopened.Snapshot(); !errors.Is(err, ErrUnavailable) {
				t.Fatal("unconfirmed recovery published", err)
			}
			reopened.write = write
			if _, err := reopened.ReserveExec(testScope, "next"); !errors.Is(err, ErrUnavailable) {
				t.Fatal("failed recovery admitted work", err)
			}
			reopened.Close()
			fresh := New(dir)
			defer fresh.Close()
			if rows, err := fresh.Snapshot(); err != nil || len(rows) != 0 {
				t.Fatal(rows, err)
			}
		})
	}
}
