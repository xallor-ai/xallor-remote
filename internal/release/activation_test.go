package release

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStageActivation_should_copyAuthenticatedBytesAndManifest(t *testing.T) {
	dir, m := bundle(t, "activation")
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := mechanicsStore(t)
	a, err := s.StageActivation(context.Background(), filepath.Join(dir, m.Artifact.Filename), m, raw)
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == "" || filepath.Dir(a.Binary) == filepath.Dir(filepath.Join(dir, m.Artifact.Filename)) {
		t.Fatal(a)
	}
	got, err := os.ReadFile(a.Binary)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join(dir, m.Artifact.Filename))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatal("activation bytes differ")
	}
	manifest, err := os.ReadFile(filepath.Join(a.Dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(manifest) != string(raw) {
		t.Fatal("original manifest bytes not retained")
	}
}

func TestStageActivation_should_reject_invalid_source_before_creating_activation(t *testing.T) {
	dir, m := bundle(t, "bad-activation")
	s := mechanicsStore(t)
	m.Artifact.SHA256 = strings.Repeat("0", 64)
	_, err := s.StageActivation(context.Background(), filepath.Join(dir, m.Artifact.Filename), m, []byte("{}"))
	if err == nil {
		t.Fatal("invalid activation accepted")
	}
	entries, _ := os.ReadDir(filepath.Join(s.Dir, "activations"))
	if len(entries) != 0 {
		t.Fatal("invalid activation left owned directory")
	}
}

func TestCleanupActivation_should_remove_only_stoppedOwnedSnapshot(t *testing.T) {
	dir, m := bundle(t, "cleanup")
	raw, _ := os.ReadFile(filepath.Join(dir, "manifest.json"))
	s := mechanicsStore(t)
	a, err := s.StageActivation(context.Background(), filepath.Join(dir, m.Artifact.Filename), m, raw)
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(s.Dir, "outside")
	os.WriteFile(outside, []byte("keep"), 0600)
	if err := s.CleanupActivation(a); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(a.Dir); !os.IsNotExist(err) {
		t.Fatal("activation retained")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatal("cleanup touched sibling")
	}
}

func TestCleanupActivation_should_refuseWhenProcessStillOwned(t *testing.T) {
	dir, m := bundle(t, "active-cleanup")
	raw, _ := os.ReadFile(filepath.Join(dir, "manifest.json"))
	s := mechanicsStore(t)
	a, err := s.StageActivation(context.Background(), filepath.Join(dir, m.Artifact.Filename), m, raw)
	if err != nil {
		t.Fatal(err)
	}
	a.PID = 123
	if err := s.CleanupActivation(a); !errors.Is(err, ErrActivationRunning) {
		t.Fatal(err)
	}
	if _, err := os.Stat(a.Dir); err != nil {
		t.Fatal("running activation removed")
	}
}
