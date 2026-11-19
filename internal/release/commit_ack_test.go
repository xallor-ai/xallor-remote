package release

import (
	"errors"
	"testing"
)

type commitAckControl struct {
	*fakeControl
	store            Store
	acknowledgements int
	refused          bool
}

func (c *commitAckControl) acknowledge() error {
	st, err := c.store.Read()
	if err != nil {
		return err
	}
	if st.Pending != nil || st.Current == nil || c.live == nil || st.Current.Artifact.SHA256 != c.live.SHA256 {
		return errors.New("ack before selection")
	}
	c.acknowledgements++
	if c.refused {
		return errors.New("child commit acknowledgement lost")
	}
	return nil
}

// A state-file commit is not proof the child latched startup admission.
// Every successful transition must await that acknowledgement before unlock.
func TestLifecycle_should_acknowledgeCommittedOwner_beforeReturningSuccess(t *testing.T) {
	first, old := bundle(t, "commit-ack-old")
	second, next := bundle(t, "commit-ack-next")
	s := mechanicsStore(t)
	c := &commitAckControl{fakeControl: control(old, next), store: s}
	for i, run := range []func() error{
		func() error { return s.EnsureRunning(first, c) },
		func() error { return s.Install(second, c) },
		func() error { return s.Rollback(c) },
	} {
		if err := run(); err != nil {
			t.Fatal(err)
		}
		if c.acknowledgements != i+1 {
			t.Fatal("transition returned without child commit acknowledgement", c.acknowledgements)
		}
	}
	c.fail = next.Artifact.SHA256
	if err := s.Install(second, c); err == nil {
		t.Fatal("failed candidate accepted")
	}
	if c.acknowledgements != 4 {
		t.Fatal("restoration skipped acknowledgement")
	}
}

// Lost acknowledgement is not permission to stop/replay an already committed
// process. Keep selection observable and report failure without automatic restore.
func TestInstall_should_preserveCommittedSelection_whenAcknowledgementIsLost(t *testing.T) {
	first, old := bundle(t, "ack-loss-old")
	second, next := bundle(t, "ack-loss-next")
	s := mechanicsStore(t)
	c := &commitAckControl{fakeControl: control(old, next), store: s}
	if err := s.Install(first, c); err != nil {
		t.Fatal(err)
	}
	stops := c.stops
	c.refused = true
	if err := s.Install(second, c); err == nil {
		t.Fatal("missing acknowledgement reported success")
	}
	st, err := s.Read()
	if err != nil || st.Current == nil || *st.Current != next || st.Pending != nil || c.live == nil || c.live.SHA256 != next.Artifact.SHA256 {
		t.Fatal("committed state lost", st, err)
	}
	if c.stops != stops+1 || c.acknowledgements != 2 {
		t.Fatal("ack loss retried or restored process")
	}
}
