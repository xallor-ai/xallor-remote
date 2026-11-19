package release

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A capture error refuses dispatch; a target path must be the activation image.
func TestShouldRefuseEffectWhenCaptureOrTargetDoesNotMatch(t *testing.T) {
	bundlePath, m := bundle(t, "evidence-target")
	s := mechanicsStore(t)
	c := control(m)
	if err := s.Install(bundlePath, c); err != nil {
		t.Fatal(err)
	}
	st, err := s.Read()
	if err != nil {
		t.Fatal(err)
	}
	st.Pending = st.Current
	st.PendingActivation = st.CurrentActivation
	st.PendingLaunchID = strings.Repeat("1", 32)
	if err := s.save(st); err != nil {
		t.Fatal(err)
	}
	trace := &effectTrace{}
	recorded := s.WithEffects(trace)
	called := false
	err = recorded.applyEffect("start", filepath.Join(s.Dir, "wrong", st.CurrentActivation, "wrong.exe"), m.Artifact.SHA256, nil, func() error { called = true; return nil })
	if !errors.Is(err, ErrCleanupUncertain) || called || len(trace.names) != 0 {
		t.Fatal(called, trace.names, err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir, "state.json"), []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	err = recorded.applyEffect("stop", "", "", nil, func() error { called = true; return nil })
	if !errors.Is(err, ErrCleanupUncertain) || called || len(trace.names) != 0 {
		t.Fatal(called, trace.names, err)
	}
}

// Empty state and legacy missing identities are observations; neither fabricates
// a start permit. Restoration targets Current with the active Pending nonce.
func TestShouldDistinguishFreshLegacyAndRestorationEvidence(t *testing.T) {
	if !ObserveSelection(State{}).Valid() {
		t.Fatal("fresh empty observation invalid")
	}
	_, m := bundle(t, "legacy-evidence")
	legacy := ObserveSelection(State{Current: &m})
	if !legacy.Valid() || legacy.Current.Complete() {
		t.Fatal(legacy)
	}
	target := SelectionIdentity{m.Artifact.SHA256, strings.Repeat("1", 32), strings.Repeat("2", 32)}
	before := ObserveSelection(State{Current: &m, CurrentActivation: target.ActivationID, Pending: &m, PendingActivation: strings.Repeat("3", 32), PendingLaunchID: target.LaunchID})
	intent := EffectIntent{Purpose: "restore", Name: "start", TargetSHA256: target.SHA256, Before: before, Target: target}
	if !intent.Valid() {
		t.Fatal("restoration lost pending nonce")
	}
	intent.Target.LaunchID = strings.Repeat("4", 32)
	if intent.Valid() {
		t.Fatal("restoration accepted old launch")
	}
}
