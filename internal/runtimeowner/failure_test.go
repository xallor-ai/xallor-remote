package runtimeowner

import (
	"errors"
	"github.com/xallor-ai/xallor-remote/internal/release"
	"strings"
	"testing"
)

// Failure to persist intent must prevent dispatch and poison this writer.
func TestPersistenceFailurePreventsDispatchAndFurtherWrites(t *testing.T) {
	j := journal(t)
	tx, err := j.Admit(request("1"))
	if err != nil {
		t.Fatal(err)
	}
	bindFixture(t, tx, boundIntent("stop", "").Before, strings.Repeat("b", 64))
	prefixFixture(t, tx, "stop")
	defer tx.Close()
	if err := tx.db.Close(); err != nil {
		t.Fatal(err)
	}
	calls := 0
	for i := 0; i < 2; i++ {
		err := testStop(tx, func() (release.StopObservation, error) { calls++; return releasedStopObservation(), nil })
		if !errors.Is(err, ErrUncertain) {
			t.Fatal(err)
		}
	}
	if calls != 0 {
		t.Fatal("dispatched without durable intent")
	}
}

// Active status must derive from the durable outcome, rather than a mutable
// second copy that could allow admission after an interrupted writer exits.
func TestActiveMarkerCannotDivergeFromDurableOutcome(t *testing.T) {
	j := journal(t)
	tx, err := j.Admit(request("1"))
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Close()
	if _, err := tx.db.Exec("UPDATE operations SET active=0 WHERE id=?", request("1").ID); err == nil {
		t.Fatal("active marker can diverge from outcome")
	}
	if err := tx.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := j.Admit(request("2")); !errors.Is(err, ErrPending) {
		t.Fatal("unfinished record was bypassed", err)
	}
}

// An applied start cannot be labelled rejected/failed without confirmed cleanup or restoration.
func TestAppliedEffectCannotBecomeCleanFailure(t *testing.T) {
	j := journal(t)
	tx, err := j.Admit(request("1"))
	if err != nil {
		t.Fatal(err)
	}
	bindFixture(t, tx, release.ObserveSelection(release.State{}), strings.Repeat("b", 64))
	prefixFixture(t, tx, "start")
	defer tx.Close()
	if err := testEffect(tx, "start", strings.Repeat("b", 64), func() (EffectState, error) { return EffectConfirmed, nil }); err != nil {
		t.Fatal(err)
	}
	for _, outcome := range []Outcome{Rejected, Failed, Succeeded} {
		if err := tx.Finish(outcome); err == nil {
			t.Fatal("unsafe outcome", outcome)
		}
	}
}

// A completed lifecycle cannot dispatch an additional attempt; no loop of
// not-applied starts may manufacture a valid repeated-launch history.
func TestEffectSequenceDoesNotDispatchRepeatedStart(t *testing.T) {
	tx, err := journal(t).Admit(request("1"))
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Close()
	bindFixture(t, tx, release.ObserveSelection(release.State{}), strings.Repeat("b", 64))
	prefixFixture(t, tx, "start")
	calls := 0
	if err := testEffect(tx, "start", strings.Repeat("b", 64), func() (EffectState, error) { calls++; return EffectNotApplied, nil }); !errors.Is(err, ErrInvalid) || calls != 1 {
		t.Fatal("not-applied start was not retained as uncertainty", calls, err)
	}
	if err := testEffect(tx, "start", strings.Repeat("b", 64), func() (EffectState, error) { calls++; return EffectNotApplied, nil }); !errors.Is(err, ErrUncertain) || calls != 1 {
		t.Fatal("repeated attempt dispatched", calls, err)
	}
}
