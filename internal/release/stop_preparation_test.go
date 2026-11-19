package release

import (
	"context"
	"errors"
	"testing"
)

type stopPreparationControl struct {
	Control
	prepare func(StopRequest) (StopAttempt, error)
}

func (c stopPreparationControl) PrepareStop(_ context.Context, r StopRequest) (StopAttempt, error) {
	return c.prepare(r)
}

type stopPreparationAttempt struct {
	*mechanicsStop
	mode  string
	cause error
}

func (a *stopPreparationAttempt) Close() ([]StopClosure, error) {
	c := StopClosure{Resource: a.observer, Closed: true}
	switch a.mode {
	case "missing":
		return nil, nil
	case "duplicate":
		return []StopClosure{c, c}, nil
	case "mismatch":
		c.Resource.Generation++
	case "unknown":
		c.Closed = false
	}
	return []StopClosure{c}, a.cause
}

// Failed durable admission must prevent PrepareStop from allocating any observers.
func TestStopPreparation_shouldRefuseAllocation_whenIntentDenied(t *testing.T) {
	cause := errors.New("intent denied")
	r := &resourceTrace{fail: cause}
	called := false
	s := mechanicsStore(t).WithEffects(nil).WithResources(r)
	err := s.stop(stopPreparationControl{prepare: func(StopRequest) (StopAttempt, error) { called = true; return nil, nil }}, State{})
	if called || !errors.Is(err, cause) || len(r.intent) != 1 || len(r.closed) != 0 {
		t.Fatal(err, r, called)
	}
}

// Partial preparation cleanup settles only exact receipts and preserves both
// preparation and physical cleanup failures; a closed group never proves stop.
func TestStopPreparation_shouldKeepUncertainty_whenPreparationFails(t *testing.T) {
	for _, mode := range []string{"exact", "nil_attempt", "missing", "duplicate", "mismatch", "unknown", "physical_error", "durable_error"} {
		t.Run(mode, func(t *testing.T) {
			cause := errors.New("capture failed")
			cleanup := errors.New("cleanup failed")
			r := &resourceTrace{}
			s := mechanicsStore(t).WithEffects(nil).WithResources(r)
			err := s.stop(stopPreparationControl{prepare: func(req StopRequest) (StopAttempt, error) {
				if len(r.intent) != 1 || r.intent[0] != req.Observer || req.Observer.AttemptID == "" || req.Observer.ActivationID != "" {
					t.Fatal("missing original group", r, req)
				}
				if mode == "nil_attempt" {
					return nil, cause
				}
				a := &stopPreparationAttempt{mechanicsStop: &mechanicsStop{observer: req.Observer}, mode: mode}
				if mode == "physical_error" {
					a.cause = cleanup
				}
				if mode == "durable_error" {
					r.fail = cleanup
				}
				return a, cause
			}}, State{})
			if !errors.Is(err, cause) || !errors.Is(err, ErrCleanupUncertain) {
				t.Fatal(err)
			}
			exact := mode == "exact" || mode == "durable_error"
			if (len(r.closed) == 1) != exact || (len(r.unknown) == 1) == exact {
				t.Fatal("incorrect group settlement", r)
			}
			if (mode == "physical_error" || mode == "durable_error") && !errors.Is(err, cleanup) {
				t.Fatal("lost cleanup failure", err)
			}
		})
	}
}
