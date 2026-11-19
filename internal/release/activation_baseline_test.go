package release

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A stopped Runtime still has one selected activation. Healthy cache bytes must
// not authorize an upgrade when that selected activation has been corrupted.
func TestInstall_should_refuseStoppedCorruptActivation_evenWithHealthyCache(t *testing.T) {
	first, old := bundle(t, "selected-old")
	second, next := bundle(t, "selected-next")
	s := mechanicsStore(t)
	c := control(old, next)
	if err := s.Install(first, c); err != nil {
		t.Fatal(err)
	}
	if err := c.Stop(); err != nil {
		t.Fatal(err)
	}
	state, err := s.Read()
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.Activation(state.CurrentActivation, old)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a.Binary, []byte("corrupt-selected-activation"), 0700); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(s.Dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	checks, stops := c.checks, c.stops
	if err := s.Install(second, c); err == nil {
		t.Fatal("healthy cache bypassed corrupt selected activation")
	}
	after, err := os.ReadFile(filepath.Join(s.Dir, "state.json"))
	if err != nil || string(after) != string(before) || c.checks != checks || c.stops != stops || c.live != nil {
		t.Fatal("refusal changed state or reached process control", err)
	}
}

// Once selected, an activation is the recovery authority. A damaged download
// cache cannot block a stopped upgrade or replace the previous activation ID.
func TestInstall_should_useStoppedActivation_whenVersionCacheIsDamaged(t *testing.T) {
	first, old := bundle(t, "cache-old")
	second, next := bundle(t, "cache-next")
	s := mechanicsStore(t)
	c := control(old, next)
	if err := s.Install(first, c); err != nil {
		t.Fatal(err)
	}
	before, err := s.Read()
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Stop(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.Path(old), []byte("damaged-cache"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureRunning("", c); err != nil {
		t.Fatal(err)
	}
	if err := s.Install(second, c); err != nil {
		t.Fatal("valid activation rejected because of cache", err)
	}
	after, err := s.Read()
	if err != nil || after.PreviousActivation != before.CurrentActivation || after.Current == nil || *after.Current != next {
		t.Fatal("selected activation was not retained", after, err)
	}
}

// Failed starts restore the exact previous activation, independently of cache
// bytes. The failure remains visible; successful restoration is not an upgrade.
func TestInstall_should_restoreSelectedActivation_whenCandidateFailsAndCacheIsDamaged(t *testing.T) {
	first, old := bundle(t, "recovery-old")
	second, next := bundle(t, "recovery-next")
	s := mechanicsStore(t)
	c := control(old, next)
	if err := s.Install(first, c); err != nil {
		t.Fatal(err)
	}
	before, err := s.Read()
	if err != nil {
		t.Fatal(err)
	}
	oldPath := c.live.Path
	if err := os.WriteFile(s.Path(old), []byte("damaged-cache"), 0700); err != nil {
		t.Fatal(err)
	}
	c.fail = next.Artifact.SHA256
	err = s.Install(second, c)
	if err == nil || !strings.Contains(err.Error(), "previous state restored") {
		t.Fatal("candidate failure did not reach verified recovery", err)
	}
	after, readErr := s.Read()
	if readErr != nil || after.Pending != nil || after.CurrentActivation != before.CurrentActivation || c.live == nil || c.live.Path != oldPath {
		t.Fatal("recovery did not restore the selected activation", after, readErr)
	}
}

// Restoration must verify the selected activation, not the optional version
// cache. This calls the recovery boundary directly so install preflight cannot
// mask a broken recovery implementation.
func TestRestore_should_ignoreVersionCache_whenSelectedActivationIsValid(t *testing.T) {
	first, old := bundle(t, "restore-cache-old")
	s := mechanicsStore(t)
	c := control(old)
	if err := s.Install(first, c); err != nil {
		t.Fatal(err)
	}
	before, err := s.Read()
	if err != nil {
		t.Fatal(err)
	}
	oldPath := c.live.Path
	if err := c.Stop(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.Path(old), []byte("damaged-cache"), 0700); err != nil {
		t.Fatal(err)
	}
	// Direct restoration still runs inside an interrupted, uniquely identified
	// launch attempt, just as the production activation path does.
	pending := before
	pending.Pending = &old
	pending.PendingActivation = before.CurrentActivation
	pending.PendingLaunchID, err = activationID()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.save(pending); err != nil {
		t.Fatal(err)
	}
	err = s.restore(before, c, errors.New("candidate readiness failed"))
	if err == nil || !strings.Contains(err.Error(), "previous state restored") || c.live == nil || c.live.Path != oldPath {
		t.Fatal("valid recovery activation was blocked by cache", err)
	}
}
