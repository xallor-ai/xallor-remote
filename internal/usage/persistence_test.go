package usage

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/atomicfile"
)

// Corrupt or obsolete envelopes cannot become empty history or allow writes
// that would erase conflict metadata; the original bytes remain for recovery.
func TestLedger_shouldFailClosedOnInvalidPersistedState(t *testing.T) {
	cases := map[string]func(*diskState){
		"missing timestamp":    func(s *diskState) { s.Events[0].At = "" },
		"invalid timestamp":    func(s *diskState) { s.Events[0].At = "invalid" },
		"non UTC timestamp":    func(s *diskState) { s.Events[0].At = "2026-11-08T00:00:00+01:00" },
		"unknown version":      func(s *diskState) { s.Version = 2 },
		"null events":          func(s *diskState) { s.Events = nil },
		"null tombstones":      func(s *diskState) { s.RetiredKeys = nil },
		"invalid hash":         func(s *diskState) { s.RetiredKeys = []string{"not-a-digest"} },
		"duplicate hash":       func(s *diskState) { s.RetiredKeys = []string{keyDigest("old"), keyDigest("old")} },
		"retained and retired": func(s *diskState) { s.RetiredKeys = []string{keyDigest(s.Events[0].IdempotencyKey)} },
		"duplicate event key":  func(s *diskState) { s.Events = append(s.Events, s.Events[0]) },
		"event bound":          func(s *diskState) { *s = usageState(MaxEvents+1, 0) },
		"tombstone bound":      func(s *diskState) { *s = usageState(0, MaxRetiredKeys+1) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			state := usageState(1, 0)
			mutate(&state)
			before := writeUsageState(t, dir, state)
			requireInvalidUsageState(t, dir, before)
		})
	}
	for _, data := range []string{`null`, `[]`, `{}`, `{"version":1,"events":[],"retired_keys":[],"command":"must-not-store"}`, `{"version":1,"events":[],"retired_keys":[]}{}`} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "usage.json"), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		requireInvalidUsageState(t, dir, []byte(data))
	}
	for _, data := range [][]byte{bytes.Repeat([]byte(" "), maxBytes+1), []byte("{\"version\":1,\"events\":[],\"retired_keys\":[],\"bad\":\"\xff\"}")} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "usage.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
		requireInvalidUsageState(t, dir, data)
	}
}

// The total byte limit covers the complete envelope, not just the event count;
// an oversized append must preserve the previous file and remain readable.
func TestLedger_shouldPreserveStateWhenEnvelopeExceedsByteBound(t *testing.T) {
	large := func(index int) Event {
		e := usageEvent("large")
		e.ID = fmt.Sprintf("%0128d", index)
		e.IdempotencyKey = "key-" + strings.Repeat("k", 112) + fmt.Sprintf("%012d", index)
		e.TenantID, e.MembershipID, e.DeviceUID, e.Operation = strings.Repeat("t", 128), strings.Repeat("m", 128), strings.Repeat("d", 128), strings.Repeat("o", 128)
		return e
	}
	state := usageState(0, 0)
	empty, _ := json.Marshal(state)
	row, _ := json.Marshal(large(0))
	count := (maxBytes - len(empty) + 1) / (len(row) + 1)
	if count >= MaxEvents {
		t.Fatal("byte-bound fixture would hit event limit first")
	}
	for i := 0; i < count; i++ {
		state.Events = append(state.Events, large(i))
	}
	dir := t.TempDir()
	before := writeUsageState(t, dir, state)
	if len(before) > maxBytes {
		t.Fatal("fixture already exceeds limit")
	}
	l := Open(dir)
	if got, err := l.Append(large(count)); !errors.Is(err, ErrLimit) || got != (Event{}) {
		t.Fatal("byte limit failed open", got, err)
	}
	after, err := os.ReadFile(l.path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("limit refusal changed disk", err)
	}
	if rows, err := l.Events(); err != nil || len(rows) != count {
		t.Fatal("limit refusal changed history", err)
	}
}

func requireInvalidUsageState(t *testing.T, dir string, before []byte) {
	t.Helper()
	l := Open(dir)
	if rows, err := l.Events(); !errors.Is(err, ErrInvalidLedger) || rows != nil {
		t.Fatal("invalid ledger became history", rows, err)
	}
	if total, err := l.CompletedUnits("tenant", "member"); !errors.Is(err, ErrInvalidLedger) || total != 0 {
		t.Fatal("invalid ledger counted", total, err)
	}
	if got, err := l.Append(usageEvent("must-not-write")); !errors.Is(err, ErrInvalidLedger) || got != (Event{}) {
		t.Fatal("invalid ledger overwritten", got, err)
	}
	after, err := os.ReadFile(l.path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("invalid state not preserved", err)
	}
}

// A persistence failure poisons the live instance. Reopen observes either the
// old complete envelope or the new complete events+tombstone, never half state.
func TestLedger_shouldPreserveAtomicRetirementOnPersistenceFailure(t *testing.T) {
	for _, committed := range []bool{false, true} {
		dir := t.TempDir()
		state := usageState(MaxEvents, 0)
		before := writeUsageState(t, dir, state)
		l := Open(dir)
		injected := errors.New("injected uncertain write")
		l.write = func(path string, data []byte) error {
			if committed {
				if err := atomicfile.Write(path, data); err != nil {
					return err
				}
			}
			return injected
		}
		if got, err := l.Append(usageEvent("new")); !errors.Is(err, injected) || got != (Event{}) {
			t.Fatal(got, err)
		}
		if len(l.retiredKeys) != 0 || l.events[0] != state.Events[0] {
			t.Fatal("failed write published memory")
		}
		if _, err := l.Events(); !errors.Is(err, injected) {
			t.Fatal("poisoned ledger kept serving", err)
		}
		if _, err := l.Append(usageEvent("later")); !errors.Is(err, injected) {
			t.Fatal("poisoned ledger kept writing", err)
		}
		reopened := Open(dir)
		if rows, err := reopened.Events(); err != nil || len(rows) != MaxEvents {
			t.Fatal("atomic envelope lost", err)
		}
		if committed {
			if _, err := reopened.Append(state.Events[0]); !errors.Is(err, ErrConflict) {
				t.Fatal("committed tombstone lost", err)
			}
			if _, err := reopened.Append(usageEvent("new")); err != nil {
				t.Fatal("committed event missing", err)
			}
		} else {
			after, err := os.ReadFile(l.path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("failed write changed old envelope", err)
			}
		}
	}
}
