package release

import (
	"context"
	"errors"
	"testing"
)

type preparedOrderAttempt struct {
	*mechanicsStop
	closeErr error
	closes   int
	ran      bool
}

func (a *preparedOrderAttempt) Close() ([]StopClosure, error) {
	if !a.closed {
		a.closes++
		a.closed = true
	}
	return []StopClosure{{Resource: a.observer, Closed: a.closeErr == nil}}, a.closeErr
}
func (a *preparedOrderAttempt) Run(ctx context.Context) (StopObservation, error) {
	a.ran = true
	return a.mechanicsStop.Run(ctx)
}

type preparedOrderControl struct {
	*fakeControl
	attempt *preparedOrderAttempt
}

func (c preparedOrderControl) PrepareStop(_ context.Context, r StopRequest) (StopAttempt, error) {
	c.attempt.observer = r.Observer
	c.attempt.intent.AttemptID = r.Observer.AttemptID
	return c.attempt, nil
}

type preparedOrderRecorder struct {
	effectTrace
	attempt *preparedOrderAttempt
	refuse  bool
	t       *testing.T
}

func (r *preparedOrderRecorder) ApplyStop(i EffectIntent, _ ResourceObligation, run func() (StopObservation, []StopClosure, error), _ func() (SelectionEvidence, error)) error {
	if r.attempt.ran {
		r.t.Fatal("dispatched before durable barrier")
	}
	if r.refuse {
		return ErrCleanupUncertain
	}
	o, _, err := run()
	if !r.attempt.closed {
		r.t.Fatal("observers retained at result recording")
	}
	if r.attempt.closeErr != nil && (err == nil || o.Confirms(*i.Stop)) {
		r.t.Fatal("close failure confirmed")
	}
	return err
}

// Journal refusal abandons prepared observers without dispatch; cleanup failure
// after dispatch must remain unknown before the recorder sees the result.
func TestPreparedStopOrdersBarrierAndObserverRelease(t *testing.T) {
	for _, tc := range []struct {
		name              string
		refuse, closeFail bool
	}{{"success", false, false}, {"intent-save", true, false}, {"close", false, true}} {
		t.Run(tc.name, func(t *testing.T) {
			s := mechanicsStore(t)
			a := &preparedOrderAttempt{mechanicsStop: &mechanicsStop{intent: StopIntent{Mode: "no_peer", AttemptID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", AbsenceBasis: "fresh"}, run: func() error { return nil }}}
			if tc.closeFail {
				a.closeErr = errors.New("observer close failed")
			}
			r := &preparedOrderRecorder{attempt: a, refuse: tc.refuse, t: t}
			s = s.WithEffects(r)
			err := s.stop(preparedOrderControl{fakeControl: control(), attempt: a}, State{})
			if (err != nil) != (tc.refuse || tc.closeFail) || a.ran == tc.refuse || a.closes != 1 {
				t.Fatal(err, a.ran, a.closes)
			}
		})
	}
}
