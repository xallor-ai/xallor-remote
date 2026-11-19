package release

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/releasetrust"
)

// Valid installed bytes remain selectable only after both provenance subjects
// have been checked; this is orchestration evidence, not a cryptographic test.
func TestActivePath_should_select_when_both_proofs_match_installed_state(t *testing.T) {
	dir, m := bundle(t, "valid-current")
	s := mechanicsStore(t)
	if err := s.Install(dir, control(m)); err != nil {
		t.Fatal(err)
	}
	var subjects []string
	s.sourceVerifier = func(_ context.Context, file string, identity releasetrust.Identity, _, _ string) error {
		if identity.Version != m.Build.Version || identity.Revision != m.Build.Revision {
			t.Fatal("wrong source identity")
		}
		subjects = append(subjects, filepath.Base(file))
		return nil
	}
	state, _ := s.Read()
	activation, err := s.Activation(state.CurrentActivation, m)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.ActivePath()
	if err != nil || got != activation.Binary {
		t.Fatalf("valid installed version rejected: %q %v", got, err)
	}
	if len(subjects) != 2 || subjects[0] != "manifest.json" || subjects[1] != m.Artifact.Filename {
		t.Fatal(subjects)
	}
}

// A cached hash must not bypass current source policy at the ensure entry point.
func TestActivePath_should_refuse_when_source_proof_fails(t *testing.T) {
	dir, m := bundle(t, "active-proof")
	s := mechanicsStore(t)
	if err := s.Install(dir, control(m)); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(s.Dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	s.sourceVerifier = func(context.Context, string, releasetrust.Identity, string, string) error {
		return releasetrust.ErrUnverified
	}
	got, err := s.ActivePath()
	if got != "" || !errors.Is(err, releasetrust.ErrUnverified) {
		t.Fatalf("unverified path returned: %q, %v", got, err)
	}
	after, err := os.ReadFile(filepath.Join(s.Dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("selection changed install state")
	}
}

// An interrupted upgrade with a previous current version still requires explicit recovery.
func TestActivePath_should_refuse_when_upgrade_is_pending(t *testing.T) {
	dir, m := bundle(t, "current")
	_, next := bundle(t, "pending")
	s := mechanicsStore(t)
	if err := s.Install(dir, control(m)); err != nil {
		t.Fatal(err)
	}
	if err := s.save(State{Current: &m, Pending: &next}); err != nil {
		t.Fatal(err)
	}
	if got, err := s.ActivePath(); err == nil || got != "" {
		t.Fatalf("pending install returned %q: %v", got, err)
	}
}

// Authenticated disk metadata must match the state used to select the version.
func TestActivePath_should_refuse_when_manifest_differs_from_state(t *testing.T) {
	dir, m := bundle(t, "metadata")
	s := mechanicsStore(t)
	if err := s.Install(dir, control(m)); err != nil {
		t.Fatal(err)
	}
	state, _ := s.Read()
	activation, err := s.Activation(state.CurrentActivation, m)
	if err != nil {
		t.Fatal(err)
	}
	changed := m
	changed.Build.BuiltAt = "different"
	writeManifest(t, activation.Dir, changed)
	if got, err := s.ActivePath(); err == nil || got != "" {
		t.Fatalf("mismatched manifest returned %q: %v", got, err)
	}
}

// A cache hit cannot repair a missing execution selection. An owner must refuse
// incomplete state rather than silently launch a second source of authority.
func TestActivePath_should_refuse_when_selected_activation_is_missing(t *testing.T) {
	dir, m := bundle(t, "missing-selection")
	s := mechanicsStore(t)
	if err := s.Install(dir, control(m)); err != nil {
		t.Fatal(err)
	}
	state, err := s.Read()
	if err != nil {
		t.Fatal(err)
	}
	state.CurrentActivation = ""
	if err := s.save(state); err != nil {
		t.Fatal(err)
	}
	if got, err := s.ActivePath(); err == nil || got != "" {
		t.Fatalf("cache replaced missing activation: %q, %v", got, err)
	}
}

// The source proof verifies quarantine copies. If the selected execution bytes
// change during that verification, selection must fail before returning a path.
func TestActivePath_should_refuse_when_activation_changes_during_proof(t *testing.T) {
	dir, m := bundle(t, "changing-selection")
	s := mechanicsStore(t)
	if err := s.Install(dir, control(m)); err != nil {
		t.Fatal(err)
	}
	state, err := s.Read()
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.Activation(state.CurrentActivation, m)
	if err != nil {
		t.Fatal(err)
	}
	s.sourceVerifier = func(context.Context, string, releasetrust.Identity, string, string) error {
		return os.WriteFile(a.Binary, []byte("changed-during-proof"), 0700)
	}
	if got, err := s.ActivePath(); err == nil || got != "" {
		t.Fatalf("changed execution bytes returned: %q, %v", got, err)
	}
}
