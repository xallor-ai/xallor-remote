package release

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/releasetrust"
)

// Legacy fakeControl tests validate lifecycle mechanics, not cryptography. The
// private injection cannot be selected by CLI, environment, manifest or proof.
func mechanicsStore(t *testing.T) Store {
	t.Helper()
	return Store{Dir: t.TempDir(), sourceVerifier: func(context.Context, string, releasetrust.Identity, string, string) error { return nil }}
}

// Authentication failure must precede every install-directory write and every
// Control method, including version probing. This checks ordering, not crypto.
func TestInstallSource_should_refuseBeforeAnyWriteCheckOrStop_whenProofFails(t *testing.T) {
	dir, m := bundle(t, "denied")
	store := Store{Dir: filepath.Join(t.TempDir(), "absent"), sourceVerifier: func(context.Context, string, releasetrust.Identity, string, string) error {
		return releasetrust.ErrUnverified
	}}
	c := control(m)
	if err := store.Install(dir, c); !errors.Is(err, releasetrust.ErrUnverified) {
		t.Fatal(err)
	}
	if c.checks != 0 || c.stops != 0 || c.runningChecks != 0 {
		t.Fatal("unverified candidate reached Control", c)
	}
	if _, err := os.Stat(store.Dir); !os.IsNotExist(err) {
		t.Fatal("authentication refusal wrote install directory", err)
	}
	if _, err := store.Stage(dir); !errors.Is(err, releasetrust.ErrUnverified) {
		t.Fatal("Stage bypassed authentication", err)
	}
	if _, err := os.Stat(store.Dir); !os.IsNotExist(err) {
		t.Fatal("Stage wrote before authentication", err)
	}
}

// Both proofs precede copying; mutations of the supplied directory after the
// snapshot is captured cannot alter installed bytes. The acceptor is a wiring
// fixture only; actual GitHub provenance is covered by the real-release E2E.
func TestInstallSource_should_copyAuthenticatedSnapshot_whenOriginalChangesDuringCheck(t *testing.T) {
	dir, m := bundle(t, "snapshot")
	store := mechanicsStore(t)
	checked := 0
	store.sourceVerifier = func(context.Context, string, releasetrust.Identity, string, string) error {
		checked++
		if checked == 1 {
			if _, err := os.Stat(filepath.Join(store.Dir, "versions")); !os.IsNotExist(err) {
				t.Fatal("copy preceded authentication")
			}
		}
		return os.WriteFile(filepath.Join(dir, m.Artifact.Filename), []byte("changed-untrusted-input"), 0700)
	}
	if err := store.Install(dir, control(m)); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(store.Path(m))
	if err != nil {
		t.Fatal(err)
	}
	if checked != 2 || string(data) != "fake-test-artifact-snapshot" {
		t.Fatal("mutable input replaced snapshot", checked)
	}
}

// A previously cached digest cannot authorize a new supplied manifest/binary.
func TestInstallSource_should_reverifyCachedInput_withoutChangingRunningState(t *testing.T) {
	dir, m := bundle(t, "cached")
	store := mechanicsStore(t)
	c := control(m)
	if err := store.Install(dir, c); err != nil {
		t.Fatal(err)
	}
	state, _ := os.ReadFile(filepath.Join(store.Dir, "state.json"))
	stops, checks := c.stops, c.checks
	store.sourceVerifier = func(context.Context, string, releasetrust.Identity, string, string) error {
		return releasetrust.ErrUnverified
	}
	if err := store.Install(dir, c); !errors.Is(err, releasetrust.ErrUnverified) {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(store.Dir, "state.json"))
	if string(state) != string(after) || c.stops != stops || c.checks != checks {
		t.Fatal("denied cached input mutated process or state")
	}
}
