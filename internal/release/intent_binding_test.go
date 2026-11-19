package release

import (
	"context"
	"errors"
	"github.com/xallor-ai/xallor-remote/internal/releasetrust"
	"os"
	"path/filepath"
	"testing"
)

type bindingTrace struct {
	effectTrace
	binding ResolvedIntent
	fail    bool
	intents []EffectIntent
}

func (e *bindingTrace) Bind(b ResolvedIntent) error {
	e.binding = b
	if e.fail {
		return errors.New("binding persistence failed")
	}
	return nil
}
func (e *bindingTrace) Apply(i EffectIntent, run func() error, observe func() (SelectionEvidence, error)) error {
	e.intents = append(e.intents, i)
	return e.effectTrace.Apply(i, run, observe)
}

// The raw digest is captured from the authenticated snapshot. Replacing the
// bundle manifest with equivalent JSON cannot replace that original identity.
func TestShouldBindAuthenticatedRawManifestBeforeLifecycleEffects(t *testing.T) {
	dir, m := bundle(t, "binding-raw")
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n', ' ')
	if err = os.WriteFile(filepath.Join(dir, "manifest.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	s := mechanicsStore(t)
	s.sourceVerifier = func(context.Context, string, releasetrust.Identity, string, string) error {
		return os.WriteFile(filepath.Join(dir, "manifest.json"), append([]byte("\n"), raw...), 0600)
	}
	tr := &bindingTrace{}
	if err = s.WithEffects(tr).Install(dir, control(m)); err != nil {
		t.Fatal(err)
	}
	st, _ := s.Read()
	if tr.binding.ManifestSHA256 != rawManifestSHA(raw) || tr.binding.Manifest != m || tr.binding.Requested.ActivationID != st.CurrentActivation || tr.binding.Requested.LaunchID != "" {
		t.Fatal(tr.binding)
	}
	if tr.intents[0].Purpose != "baseline_prepare" || tr.intents[len(tr.intents)-1].Purpose != "requested" {
		t.Fatal(tr.intents)
	}
}

// Failed binding must prevent selection writes and Stop/Start, while preserving
// uncertainty as required by the existing source/activation cleanup contract.
func TestShouldPreventLifecycleEffectsWhenBindingFails(t *testing.T) {
	dir, m := bundle(t, "binding-denied")
	s := mechanicsStore(t)
	c := control(m)
	tr := &bindingTrace{fail: true}
	if err := s.WithEffects(tr).Install(dir, c); !errors.Is(err, ErrCleanupUncertain) {
		t.Fatal(err)
	}
	st, _ := s.Read()
	if c.stops != 0 || st.Current != nil || st.Pending != nil || len(tr.names) != 0 {
		t.Fatal(c.stops, st, tr.names)
	}
}

// Candidate failure preserves the requested source and marks the independently
// restored baseline explicitly, rather than replacing the request binding.
func TestShouldKeepRequestedCandidateWhileRestoringBaseline(t *testing.T) {
	first, old := bundle(t, "bound-old")
	second, next := bundle(t, "bound-next")
	s := mechanicsStore(t)
	c := control(old, next)
	if err := s.Install(first, c); err != nil {
		t.Fatal(err)
	}
	baseline, _ := s.Read()
	c.fail = next.Artifact.SHA256
	tr := &bindingTrace{}
	if err := s.WithEffects(tr).Install(second, c); err == nil {
		t.Fatal("candidate failure lost")
	}
	st, _ := s.Read()
	if tr.binding.Manifest != next || tr.binding.Baseline != ObserveSelection(baseline) || st.Current.Artifact.SHA256 != old.Artifact.SHA256 {
		t.Fatal(tr.binding, st)
	}
	n := len(tr.intents)
	for _, i := range tr.intents[n-3:] {
		if i.Purpose != "restore" {
			t.Fatal(i)
		}
	}
	if st.CurrentLaunchID == baseline.CurrentLaunchID {
		t.Fatal("restoration reused old launch")
	}
}

// A live unmanaged baseline has no retained launch identity. Tracked install
// must reject before creating candidate/adoption activations or invoking Stop.
func TestShouldRejectUnmanagedBaselineBeforeTrackedStaging(t *testing.T) {
	first, old := bundle(t, "unmanaged-old")
	second, next := bundle(t, "unmanaged-next")
	s := mechanicsStore(t)
	c := control(old, next)
	if err := c.Start(filepath.Join(first, old.Artifact.Filename), old.Artifact.SHA256); err != nil {
		t.Fatal(err)
	}
	tr := &bindingTrace{}
	err := s.WithEffects(tr).Install(second, c)
	if err == nil || c.stops != 0 || len(tr.names) != 0 {
		t.Fatal(err, c.stops, tr.names)
	}
	if _, statErr := os.Stat(filepath.Join(s.Dir, "activations")); !os.IsNotExist(statErr) {
		t.Fatal("unmanaged baseline reached staging", statErr)
	}
}

// Fresh stop refusal emits an explicit cancellation of the exact empty state;
// no start or restoration launch may follow a pre-dispatch refusal.
func TestShouldCancelFreshInstallWithoutStartingRuntime(t *testing.T) {
	dir, m := bundle(t, "fresh-cancel")
	s := mechanicsStore(t)
	c := &uncertainControl{fakeControl: control(m), stopRejected: true}
	tr := &bindingTrace{}
	err := s.WithEffects(tr).Install(dir, c)
	if err == nil || errors.Is(err, ErrCleanupUncertain) {
		t.Fatal(err)
	}
	st, _ := s.Read()
	if ObserveSelection(st) != ObserveSelection(State{}) || len(c.starts) != 0 {
		t.Fatal(st, c.starts)
	}
	last := tr.intents[len(tr.intents)-1]
	if last.Name != "clear" || last.Purpose != "cancel" || *last.Expected != tr.binding.Baseline {
		t.Fatal(last)
	}
}
