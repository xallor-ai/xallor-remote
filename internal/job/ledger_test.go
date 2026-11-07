package job

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/invocation"
)

var testScope = invocation.Scope{Authority: strings.Repeat("a", 32), DeviceUID: strings.Repeat("b", 32), GrantID: strings.Repeat("c", 32)}

// Competing callers atomically reserve a single shared capacity across job
// kinds. Handoff also retains its own smaller process-management hard ceiling.
func TestLedger_should_boundConcurrentReservations_acrossJobKinds(t *testing.T) {
	l := New(t.TempDir())
	defer l.Close()
	var wg sync.WaitGroup
	owners := make(chan *Owner, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			var o *Owner
			var err error
			if i%2 == 0 {
				o, err = l.ReserveHandoff(testScope, strconv.Itoa(i))
			} else {
				o, err = l.ReserveExec(testScope, strconv.Itoa(i))
			}
			if err == nil {
				owners <- o
			} else if !errors.Is(err, ErrCapacity) {
				t.Errorf("reserve: %v", err)
			}
		}(i)
	}
	wg.Wait()
	close(owners)
	if len(owners) != Capacity {
		t.Fatalf("reserved %d", len(owners))
	}
	rows, err := l.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	handoffs := 0
	for _, r := range rows {
		if r.Kind == "handoff" {
			handoffs++
		}
	}
	if handoffs > HandoffCapacity {
		t.Fatal("handoff capacity exceeded")
	}
	for o := range owners {
		if o.Finish() != nil {
			t.Fatal("release failed")
		}
	}
	rows, err = l.Snapshot()
	if err != nil || len(rows) != 0 {
		t.Fatal(rows, err)
	}
}

// A lost Runtime leaves persisted occupancy uncertain. Reopening and repeated
// queries must never release it or permit reuse of the same scoped operation.
func TestLedger_should_keepOccupancy_whenRuntimeRestarts(t *testing.T) {
	dir := t.TempDir()
	l := New(dir)
	for i := 0; i < Capacity; i++ {
		o, err := l.ReserveExec(testScope, strconv.Itoa(i))
		if err != nil {
			t.Fatal(err)
		}
		if err := l.prepareStart(o.ID()); err != nil {
			t.Fatal(err)
		}
	}
	l.Close()
	l = New(dir)
	defer l.Close()
	rows, err := l.Snapshot()
	if err != nil || len(rows) != Capacity {
		t.Fatal(rows, err)
	}
	for _, r := range rows {
		if r.State != "uncertain" {
			t.Fatal(r)
		}
	}
	if _, err = l.ReserveExec(testScope, "new"); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if _, err = l.ReserveExec(testScope, "0"); !errors.Is(err, ErrUncertain) {
		t.Fatal(err)
	}
}

// Persisting the intent is mandatory before returning a launch capability.
// A possibly-committed write failure poisons this owner, never retries blindly.
func TestLedger_should_rejectLaunch_whenPersistenceFails(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(strconv.FormatBool(committed), func(t *testing.T) {
			l := New(t.TempDir())
			defer l.Close()
			write := l.write
			l.write = func(path string, b []byte) error {
				if committed {
					if err := write(path, b); err != nil {
						return err
					}
				}
				return errors.New("lost write result")
			}
			if o, err := l.ReserveExec(testScope, "one"); o != nil || !errors.Is(err, ErrUnavailable) {
				t.Fatal(o, err)
			}
			l.write = write
			if _, err := l.ReserveExec(testScope, "two"); !errors.Is(err, ErrUnavailable) {
				t.Fatal("write uncertainty recovered without reopening")
			}
		})
	}
}

// An unresolved cleanup blocks new work on that session even if the request ID
// changes. This internal occupancy is separate from the Agent response state.
func TestLedger_should_retainUncertainOwner_and_releaseConfirmedOwnerOnce(t *testing.T) {
	l := New(t.TempDir())
	defer l.Close()
	o, err := l.ReserveHandoff(testScope, "session")
	if err != nil {
		t.Fatal(err)
	}
	o.uncertain = true
	if !errors.Is(o.Finish(), ErrUncertain) || !errors.Is(o.Finish(), ErrUncertain) {
		t.Fatal("uncertain owner freed")
	}
	if _, err = l.ReserveHandoff(testScope, "session"); !errors.Is(err, ErrUncertain) {
		t.Fatal(err)
	}
	other, err := l.ReserveExec(testScope, "other")
	if err != nil {
		t.Fatal(err)
	}
	if other.Finish() != nil || other.Finish() != nil {
		t.Fatal("release not idempotent")
	}
	rows, err := l.Snapshot()
	if err != nil || len(rows) != 1 || rows[0].State != "uncertain" {
		t.Fatal(rows, err)
	}
}

// Neither a second Runtime nor malformed disk state may reset resource debt.
func TestLedger_should_failClosed_whenLockedOrCorrupt(t *testing.T) {
	dir := t.TempDir()
	first := New(dir)
	defer first.Close()
	if _, err := first.ReserveExec(testScope, "one"); err != nil {
		t.Fatal(err)
	}
	second := New(dir)
	defer second.Close()
	if _, err := second.Snapshot(); !errors.Is(err, ErrUnavailable) {
		t.Fatal("second ledger opened", err)
	}
	first.Close()
	if err := os.WriteFile(filepath.Join(dir, "jobs.json"), []byte(`{"version":1,"jobs":{}} {}`), 0600); err != nil {
		t.Fatal(err)
	}
	third := New(dir)
	defer third.Close()
	if _, err := third.Snapshot(); !errors.Is(err, ErrUnavailable) {
		t.Fatal("trailing corruption ignored", err)
	}
}

// Disk entries preserve trusted identity without retaining shell, prompt or
// result data. A valid envelope with an invalid scope must not erase occupancy.
func TestLedger_should_validateStoredIdentity(t *testing.T) {
	dir := t.TempDir()
	id := strings.Repeat("d", 32)
	b, _ := json.Marshal(diskState{Version: 1, Jobs: map[string]Record{id: {ID: id, Kind: "exec", Key: "one", State: "occupied"}}})
	if err := os.WriteFile(filepath.Join(dir, "jobs.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	l := New(dir)
	defer l.Close()
	if _, err := l.Snapshot(); !errors.Is(err, ErrUnavailable) {
		t.Fatal("invalid identity accepted", err)
	}
}
