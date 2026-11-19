package release

import (
	"errors"
	"reflect"
	"testing"
)

type effectTrace struct {
	names  []string
	refuse func(string, string) bool
}

func (e *effectTrace) Bind(ResolvedIntent) error { return nil }

func (e *effectTrace) Apply(intent EffectIntent, run func() error, observe func() (SelectionEvidence, error)) error {
	name, sha := intent.Name, intent.TargetSHA256
	e.names = append(e.names, name)
	if e.refuse != nil && e.refuse(name, sha) {
		return ErrCleanupUncertain
	}
	return run()
}

// Install and rollback must pass every externally visible lifecycle effect
// through the same recorder, including the selection write before ready.
func TestLifecycleRecorderCoversInstallAndRollback(t *testing.T) {
	first, old := bundle(t, "record-old")
	second, next := bundle(t, "record-next")
	s := mechanicsStore(t)
	c := control(old, next)
	if err := s.Install(first, c); err != nil {
		t.Fatal(err)
	}
	for _, run := range []func(Store) error{func(s Store) error { return s.Install(second, c) }, func(s Store) error { return s.Rollback(c) }} {
		trace := &effectTrace{}
		if err := run(s.WithEffects(trace)); err != nil {
			t.Fatal(err)
		}
		want := []string{"prepare", "stop", "start", "commit", "ready"}
		if len(trace.names) < len(want) || !reflect.DeepEqual(trace.names[len(trace.names)-len(want):], want) {
			t.Fatal(trace.names)
		}
	}
}

// A rejected start intent must never start a candidate or automatically restore.
// An unconfirmed commit after a started candidate must not stop/replay it either.
func TestLifecycleRecorderFailurePreventsFurtherProcessEffects(t *testing.T) {
	for _, blocked := range []string{"start", "commit"} {
		t.Run(blocked, func(t *testing.T) {
			first, old := bundle(t, "blocked-old")
			second, next := bundle(t, "blocked-next")
			s := mechanicsStore(t)
			c := &uncertainControl{fakeControl: control(old, next)}
			if err := s.Install(first, c); err != nil {
				t.Fatal(err)
			}
			c.starts = nil
			beforeStops := c.stops
			trace := &effectTrace{refuse: func(name, sha string) bool { return name == blocked && sha == next.Artifact.SHA256 }}
			if err := s.WithEffects(trace).Install(second, c); !errors.Is(err, ErrCleanupUncertain) {
				t.Fatal(err)
			}
			wantStarts := 0
			if blocked == "commit" {
				wantStarts = 1
			}
			if len(c.starts) != wantStarts || c.stops != beforeStops+1 {
				t.Fatal("effects continued after recording failed", c.starts, c.stops)
			}
			st, err := s.Read()
			if err != nil || st.Pending == nil || st.Pending.Artifact.SHA256 != next.Artifact.SHA256 {
				t.Fatal(st, err)
			}
		})
	}
}

// Deterministic candidate cleanup retains the one verified restoration path,
// and restoration itself records its start, commit and ready acknowledgement.
func TestLifecycleRecorderIncludesVerifiedRestoration(t *testing.T) {
	first, old := bundle(t, "restore-record-old")
	second, next := bundle(t, "restore-record-next")
	s := mechanicsStore(t)
	c := control(old, next)
	if err := s.Install(first, c); err != nil {
		t.Fatal(err)
	}
	c.fail = next.Artifact.SHA256
	trace := &effectTrace{}
	if err := s.WithEffects(trace).Install(second, c); err == nil {
		t.Fatal("candidate failure lost")
	}
	want := []string{"start", "start", "commit", "ready"}
	if len(trace.names) < len(want) || !reflect.DeepEqual(trace.names[len(trace.names)-len(want):], want) {
		t.Fatal(trace.names)
	}
}

// A proven pre-dispatch stop refusal restores the old selection and observes
// that owner again, permitting a truthful failed receipt without a stuck journal.
func TestLifecycleRecorderAcknowledgesBaselineAfterStopRefusal(t *testing.T) {
	first, old := bundle(t, "cancel-old")
	second, next := bundle(t, "cancel-next")
	s := mechanicsStore(t)
	c := &uncertainControl{fakeControl: control(old, next)}
	if err := s.Install(first, c); err != nil {
		t.Fatal(err)
	}
	c.stopRejected = true
	trace := &effectTrace{}
	if err := s.WithEffects(trace).Install(second, c); err == nil {
		t.Fatal("refusal lost")
	}
	n := len(trace.names)
	if n < 2 || trace.names[n-2] != "commit" || trace.names[n-1] != "ready" {
		t.Fatal("baseline not acknowledged after cancellation", trace.names)
	}
}
