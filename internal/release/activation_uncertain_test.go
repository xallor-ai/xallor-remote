package release

import (
	"errors"
	"fmt"
	"testing"
)

type uncertainControl struct {
	*fakeControl
	stopUncertain bool
	stopRejected  bool
	uncertainSHA  string
	starts        []string
}

func (c *uncertainControl) Stop() error {
	if c.stopRejected {
		return errors.New("stop rejected before dispatch")
	}
	if c.stopUncertain {
		return fmt.Errorf("stop acknowledgement lost: %w", ErrCleanupUncertain)
	}
	return c.fakeControl.Stop()
}

// A failed explicit recovery must not erase the pending transition that led to
// recovery. A pre-dispatch refusal preserves the exact previously stored state.
func TestRollback_should_preserveExistingPending_whenStopRejectedBeforeDispatch(t *testing.T) {
	first, old := bundle(t, "recovery-stop-old")
	_, next := bundle(t, "recovery-stop-next")
	s := mechanicsStore(t)
	c := &uncertainControl{fakeControl: control(old, next)}
	if err := s.Install(first, c); err != nil {
		t.Fatal(err)
	}
	st, err := s.Read()
	if err != nil {
		t.Fatal(err)
	}
	st.Pending, st.PendingActivation = &next, "0123456789abcdef0123456789abcdef"
	if err := s.save(st); err != nil {
		t.Fatal(err)
	}
	c.stopRejected = true
	if err := s.Rollback(c); err == nil {
		t.Fatal("rejected recovery stop accepted")
	}
	after, err := s.Read()
	if err != nil || after.Pending == nil || *after.Pending != next || after.PendingActivation != st.PendingActivation {
		t.Fatal("recovery refusal erased prior pending state", after, err)
	}
}

// Recovery cannot overwrite the identity of a possibly running failed
// candidate before that candidate's stop is confirmed.
func TestRollback_should_retainUncertainCandidateIdentity_untilStopIsConfirmed(t *testing.T) {
	first, old := bundle(t, "pending-owner-old")
	second, next := bundle(t, "pending-owner-next")
	s := mechanicsStore(t)
	c := &uncertainControl{fakeControl: control(old, next)}
	if err := s.Install(first, c); err != nil {
		t.Fatal(err)
	}
	c.uncertainSHA = next.Artifact.SHA256
	if err := s.Install(second, c); !errors.Is(err, ErrCleanupUncertain) {
		t.Fatal(err)
	}
	before, err := s.Read()
	if err != nil {
		t.Fatal(err)
	}
	c.stopUncertain = true
	c.starts = nil
	if err := s.Rollback(c); !errors.Is(err, ErrCleanupUncertain) {
		t.Fatal(err)
	}
	after, err := s.Read()
	if err != nil || after.Pending == nil || *after.Pending != next || after.PendingActivation != before.PendingActivation {
		t.Fatal("uncertain candidate identity was replaced before stop", after, err)
	}
	if len(c.starts) != 0 || c.live == nil || c.live.SHA256 != next.Artifact.SHA256 {
		t.Fatal("recovery restarted while stop was uncertain")
	}
}

func (c *uncertainControl) Start(path, sha string) error {
	c.starts = append(c.starts, sha)
	if err := c.fakeControl.Start(path, sha); err != nil {
		return err
	}
	if sha == c.uncertainSHA {
		return fmt.Errorf("candidate may still be alive: %w", ErrCleanupUncertain)
	}
	return nil
}

// A possibly live failed candidate must not cause an automatic previous-version
// Start. Preserve pending and the baseline for explicit, observed recovery.
func TestInstall_should_preservePending_withoutRestoreWhenCandidateCleanupIsUnknown(t *testing.T) {
	first, old := bundle(t, "uncertain-old")
	second, next := bundle(t, "uncertain-next")
	s := mechanicsStore(t)
	c := &uncertainControl{fakeControl: control(old, next)}
	if err := s.Install(first, c); err != nil {
		t.Fatal(err)
	}
	c.starts = nil
	c.uncertainSHA = next.Artifact.SHA256
	if err := s.Install(second, c); err == nil {
		t.Fatal("unknown startup reported success")
	}
	st, err := s.Read()
	if err != nil || st.Pending == nil || *st.Pending != next || st.Current == nil || *st.Current != old {
		t.Fatal("unknown process state was cleared", st, err)
	}
	if len(c.starts) != 1 || c.starts[0] != next.Artifact.SHA256 || c.live == nil || c.live.SHA256 != next.Artifact.SHA256 {
		t.Fatal("old version was started while candidate may still run", c.starts)
	}
}

// A stop result without confirmed termination cannot clear the transition or
// start the candidate, even when the old IPC process appears healthy again.
func TestInstall_should_preservePending_whenBaselineStopIsUnconfirmed(t *testing.T) {
	first, old := bundle(t, "stop-old")
	second, next := bundle(t, "stop-next")
	s := mechanicsStore(t)
	c := &uncertainControl{fakeControl: control(old, next)}
	if err := s.Install(first, c); err != nil {
		t.Fatal(err)
	}
	c.starts = nil
	c.stopUncertain = true
	if err := s.Install(second, c); err == nil {
		t.Fatal("unconfirmed stop accepted")
	}
	st, err := s.Read()
	if err != nil || st.Pending == nil || *st.Pending != next || st.Current == nil || *st.Current != old {
		t.Fatal("stop uncertainty was lost", st, err)
	}
	if len(c.starts) != 0 || c.live == nil || c.live.SHA256 != old.Artifact.SHA256 {
		t.Fatal("candidate executed after unknown stop")
	}
}
