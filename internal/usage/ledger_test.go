package usage

import (
	"errors"
	"path/filepath"
	"testing"
)

// Privacy and idempotency: usage contains only bounded scope/outcome metadata,
// duplicate delivery is harmless, and a conflicting reuse cannot overwrite.
func TestLedger_shouldMinimizeAndDeduplicateUsageEvents(t *testing.T) {
	l := Open(t.TempDir())
	e := Event{ID: "event-1", IdempotencyKey: "route-1", TenantID: "tenant", MembershipID: "member", DeviceUID: "device", Operation: "exec", Outcome: "completed", Units: 3}
	if _, err := l.Append(e); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Append(e); err != nil {
		t.Fatal(err)
	}
	conflict := e
	conflict.Units = 9
	if _, err := l.Append(conflict); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if units, err := l.CompletedUnits("tenant", "member"); err != nil || units != 3 {
		t.Fatal(units, err)
	}
	rows, err := Open(filepath.Dir(l.path)).Events()
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows=%d err=%v", len(rows), err)
	}
}

// Unknown or failed outcomes are retained for reconciliation but never enter
// completed usage, so a lost terminal cannot become a billable success.
func TestLedger_shouldNeverCountUnknownOrFailedAsCompleted(t *testing.T) {
	l := Open(t.TempDir())
	for _, outcome := range []string{"unknown", "failed"} {
		if _, err := l.Append(Event{ID: outcome, IdempotencyKey: outcome, TenantID: "tenant", MembershipID: "member", DeviceUID: "device", Operation: "exec", Outcome: outcome, Units: 10}); err != nil {
			t.Fatal(err)
		}
	}
	if units, err := l.CompletedUnits("tenant", "member"); err != nil || units != 0 {
		t.Fatal(units, err)
	}
}
