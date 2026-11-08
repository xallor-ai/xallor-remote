package usage

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func usageEvent(id string) Event {
	return Event{ID: "event-" + id, At: "2026-11-08T00:00:00Z", IdempotencyKey: "key-" + id, TenantID: "tenant", MembershipID: "member", DeviceUID: "device", Operation: "exec", Outcome: "completed", Units: 1}
}

func writeUsageState(t *testing.T, dir string, state diskState) []byte {
	t.Helper()
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "usage.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	return data
}

func usageState(events, retired int) diskState {
	state := diskState{Version: 1, Events: []Event{}, RetiredKeys: []string{}}
	for i := 0; i < events; i++ {
		state.Events = append(state.Events, usageEvent(fmt.Sprint(i)))
	}
	for i := 0; i < retired; i++ {
		state.RetiredKeys = append(state.RetiredKeys, keyDigest(fmt.Sprintf("retired-%d", i)))
	}
	return state
}

// Explicit timestamps must use the canonical RFC3339Nano UTC representation
// within absolute bounds; omission is filled once and reused by duplicates.
func TestLedger_shouldValidateCanonicalUTCTimeAndFillMissingAt(t *testing.T) {
	for _, stamp := range []string{"1970-01-01T00:00:00Z", "2026-11-08T01:02:03.123456789Z", "2026-11-08T01:02:03.1Z", "9999-12-31T23:59:59.999999999Z"} {
		l := Open(t.TempDir())
		e := usageEvent("valid")
		e.At = stamp
		if got, err := l.Append(e); err != nil || got.At != stamp {
			t.Fatalf("valid At: %+v %v", got, err)
		}
	}
	for _, stamp := range []string{"garbage", "2026-11-08", "1969-12-31T23:59:59Z", "10000-01-01T00:00:00Z", "2026-11-08T01:02:03+00:00", "2026-11-08T01:02:03-00:00", "2026-11-08T01:02:03+01:00", "2026-11-08T01:02:03.100Z", "2026-11-08T01:02:03,1Z", "2026-11-08T01:02:03.1234567891Z", "2026-11-08T01:02:03Z\x00"} {
		l := Open(t.TempDir())
		e := usageEvent("invalid")
		e.At = stamp
		if got, err := l.Append(e); !errors.Is(err, ErrInvalidEvent) || got != (Event{}) {
			t.Fatalf("invalid At accepted: %q %+v %v", stamp, got, err)
		}
		if rows, err := l.Events(); err != nil || len(rows) != 0 {
			t.Fatal("invalid event published", rows, err)
		}
		if _, err := os.Stat(l.path); !os.IsNotExist(err) {
			t.Fatal("invalid event wrote ledger", err)
		}
	}
	l := Open(t.TempDir())
	e := usageEvent("automatic")
	e.At = ""
	first, err := l.Append(e)
	if err != nil {
		t.Fatal(err)
	}
	stamp, err := time.Parse(time.RFC3339Nano, first.At)
	if err != nil || stamp.UTC().Format(time.RFC3339Nano) != first.At {
		t.Fatal("generated invalid At", first.At, err)
	}
	if again, err := l.Append(e); err != nil || again != first {
		t.Fatal("duplicate regenerated timestamp", again, err)
	}
	if again, err := Open(filepath.Dir(l.path)).Append(e); err != nil || again != first {
		t.Fatal("reopen changed duplicate timestamp", again, err)
	}
}

// A scoped sum can reach MaxInt64 exactly, but exceeding it returns an error
// and zero. Failed/unknown events and other scopes never contribute.
func TestCompletedUnits_shouldFailClosedOnOverflowWithoutPartialTotal(t *testing.T) {
	l := Open(t.TempDir())
	maximum := usageEvent("maximum")
	maximum.Units = math.MaxInt64
	if _, err := l.Append(maximum); err != nil {
		t.Fatal(err)
	}
	for _, outcome := range []string{"failed", "unknown"} {
		e := usageEvent(outcome)
		e.Outcome, e.Units = outcome, math.MaxInt64
		if _, err := l.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	for _, field := range []string{"tenant", "membership"} {
		e := usageEvent("other-" + field)
		e.Units = math.MaxInt64
		if field == "tenant" {
			e.TenantID = "other"
		} else {
			e.MembershipID = "other"
		}
		if _, err := l.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	zero := usageEvent("zero")
	zero.Units = 0
	if _, err := l.Append(zero); err != nil {
		t.Fatal(err)
	}
	if total, err := l.CompletedUnits("tenant", "member"); err != nil || total != math.MaxInt64 {
		t.Fatal(total, err)
	}
	if _, err := l.Append(usageEvent("overflow")); err != nil {
		t.Fatal(err)
	}
	for _, ledger := range []*Ledger{l, Open(filepath.Dir(l.path))} {
		if total, err := ledger.CompletedUnits("tenant", "member"); !errors.Is(err, ErrOverflow) || total != 0 {
			t.Fatal("overflow returned partial/wrapped count", total, err)
		}
		if rows, err := ledger.Events(); err != nil || len(rows) != 7 {
			t.Fatal("aggregate failure destroyed observations", len(rows), err)
		}
		if total, err := ledger.CompletedUnits("absent", "member"); err != nil || total != 0 {
			t.Fatal(total, err)
		}
	}
}

// Eviction must persist only a digest tombstone and reject even an identical
// old event after reopen, while retained exact duplicates remain harmless.
func TestLedger_shouldRememberRetiredKeysAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	state := usageState(MaxEvents, 0)
	writeUsageState(t, dir, state)
	l := Open(dir)
	if _, err := l.Append(usageEvent("new")); err != nil {
		t.Fatal(err)
	}
	for _, ledger := range []*Ledger{l, Open(dir)} {
		if got, err := ledger.Append(state.Events[0]); !errors.Is(err, ErrConflict) || got != (Event{}) {
			t.Fatal("retired key replayed", got, err)
		}
		old := state.Events[0]
		old.Units++
		if _, err := ledger.Append(old); !errors.Is(err, ErrConflict) {
			t.Fatal("changed retired key replayed", err)
		}
		if got, err := ledger.Append(state.Events[1]); err != nil || got != state.Events[1] {
			t.Fatal("retained duplicate failed", got, err)
		}
		if len(ledger.events) != MaxEvents || len(ledger.retiredKeys) != 1 || ledger.retiredKeys[0] != keyDigest(state.Events[0].IdempotencyKey) {
			t.Fatal("wrong retention bounds")
		}
	}
	data, err := os.ReadFile(l.path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(state.Events[0].IdempotencyKey)) || bytes.Contains(data, []byte(state.Events[0].ID)) {
		t.Fatal("retired payload retained instead of digest")
	}
}

// Saturated conflict metadata cannot be discarded to admit another event.
// Rejections do not change disk; existing duplicates and reads still work.
func TestLedger_shouldRejectFurtherRetirementWhenTombstonesAreFull(t *testing.T) {
	dir := t.TempDir()
	state := usageState(MaxEvents, MaxRetiredKeys)
	before := writeUsageState(t, dir, state)
	for _, ledger := range []*Ledger{Open(dir), Open(dir)} {
		if got, err := ledger.Append(usageEvent("overflow-retirement")); !errors.Is(err, ErrRetentionFull) || got != (Event{}) {
			t.Fatal("forgot tombstone at capacity", got, err)
		}
		if rows, err := ledger.Events(); err != nil || len(rows) != MaxEvents || rows[0] != state.Events[0] {
			t.Fatal("full rejection changed history", err)
		}
		if _, err := ledger.Append(state.Events[0]); err != nil {
			t.Fatal("full metadata blocked retained duplicate", err)
		}
		retired := usageEvent("retired")
		retired.IdempotencyKey = "retired-0"
		if _, err := ledger.Append(retired); !errors.Is(err, ErrConflict) {
			t.Fatal("full metadata forgot key", err)
		}
		after, err := os.ReadFile(ledger.path)
		if err != nil || !bytes.Equal(after, before) {
			t.Fatal("full metadata mutated disk", err)
		}
	}
}

// Metadata saturation only prevents new retirement, not use of an existing
// free event slot. The next eviction must still stop without forgetting keys.
func TestLedger_shouldUseRemainingEventSlotWithoutResettingFullMetadata(t *testing.T) {
	dir := t.TempDir()
	writeUsageState(t, dir, usageState(MaxEvents-1, MaxRetiredKeys))
	l := Open(dir)
	if _, err := l.Append(usageEvent("last-slot")); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir).Append(usageEvent("requires-retirement")); !errors.Is(err, ErrRetentionFull) {
		t.Fatal("metadata reset", err)
	}
}
