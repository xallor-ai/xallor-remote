package runtimeowner

import (
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/xallor-ai/xallor-remote/internal/release"
	"sync"
)

type Transaction struct {
	mu               sync.Mutex
	db               *sql.DB
	unlock           func()
	record           Record
	last             []byte
	namespace        string
	revision         int64
	closed, poisoned bool
	running          bool
}

// Effect persists intent before calling apply. Only an independent observation
// may return NotApplied; an ordinary error must return Unknown, not imply retry.
// A panic deliberately leaves the pre-dispatch marker durable.
func (t *Transaction) Effect(intent release.EffectIntent, apply func() (EffectState, *release.SelectionEvidence, error)) error {
	return t.effect(intent, apply, nil)
}
func (t *Transaction) effect(intent release.EffectIntent, apply func() (EffectState, *release.SelectionEvidence, error), stopObservation **release.StopObservation) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed || t.poisoned || t.record.Outcome != Pending || t.record.unresolved() {
		return ErrUncertain
	}
	if intent.Expected != nil {
		copy := *intent.Expected
		intent.Expected = &copy
	}
	if intent.Stop != nil {
		copy := *intent.Stop
		intent.Stop = &copy
	}
	e := Effect{EffectIntent: intent, State: EffectPending}
	if !e.valid() || apply == nil {
		return ErrInvalid
	}
	next := t.record
	next.Effects = append(append([]Effect(nil), t.record.Effects...), e)
	if err := t.save(next); err != nil {
		return err
	}
	state, observed, cause := apply()
	if observed != nil {
		copy := *observed
		observed = &copy
	}
	if observed != nil && !observed.Valid() {
		observed = nil
	}
	if state != EffectConfirmed && state != EffectNotApplied && !(intent.Name == "start" && state == EffectQuiescentFailure) {
		state = EffectUnknown
	}
	if observed == nil || !intent.MatchesObserved(*observed) {
		state = EffectUnknown
	}
	next.Effects = append([]Effect(nil), next.Effects...)
	if intent.Name == "stop" {
		var stop *release.StopObservation
		if stopObservation != nil && *stopObservation != nil {
			copy := **stopObservation
			if copy.Valid(*intent.Stop) {
				stop = &copy
			}
		}
		next.Effects[len(next.Effects)-1].StopObserved = stop
		if stop == nil || !((state == EffectConfirmed && stop.Confirms(*intent.Stop)) || (state == EffectNotApplied && stop.NotDispatched(*intent.Stop))) {
			state = EffectUnknown
		}
	}
	next.Effects[len(next.Effects)-1].State = state
	next.Effects[len(next.Effects)-1].Observed = observed
	if state == EffectUnknown {
		next.Outcome = Uncertain
	}
	if err := t.save(next); err != nil {
		return errors.Join(ErrUncertain, cause, err)
	}
	if state == EffectUnknown {
		return errors.Join(ErrUncertain, cause)
	}
	return cause
}

// Finish records only observations already established by the owner. It cannot
// turn an interrupted effect into a successful or cleanly failed transaction.
func (t *Transaction) Finish(outcome Outcome) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.running {
		return ErrRecorded
	}
	if t.closed || t.poisoned || t.record.Outcome != Pending {
		return ErrUncertain
	}
	if outcome == Pending {
		return ErrInvalid
	}
	if t.hasUnsettledResourceLocked() {
		return ErrUncertain
	}
	next := t.record
	next.Outcome = outcome
	if !next.valid() {
		return ErrInvalid
	}
	return t.save(next)
}

func (t *Transaction) save(next Record) error {
	if !next.valid() || next.NamespaceSHA256 != t.namespace {
		return ErrInvalid
	}
	raw, err := json.Marshal(next)
	if err != nil || len(raw) > maxRecordBytes {
		return ErrInvalid
	}
	previousActive := 0
	if t.record.active() {
		previousActive = 1
	}
	nextRevision := t.revision + 1
	result, err := t.db.Exec("UPDATE operations SET record=?,revision=? WHERE id=? AND record=? AND active=? AND revision=?", raw, nextRevision, next.Request.ID, t.last, previousActive, t.revision)
	if err == nil {
		var n int64
		n, err = result.RowsAffected()
		if err == nil && n != 1 {
			err = ErrUncertain
		}
	}
	if err != nil {
		t.poisoned = true
		return errors.Join(ErrUncertain, err)
	}
	t.revision = nextRevision
	t.record, t.last = next, raw
	return nil
}

// Close releases resources without inferring a terminal outcome or resuming work.
func (t *Transaction) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil
	}
	t.closed = true
	err := t.db.Close()
	t.unlock()
	return err
}
