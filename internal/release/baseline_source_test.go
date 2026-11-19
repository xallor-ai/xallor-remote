package release

import (
	"context"
	"errors"
	"fmt"
	"github.com/xallor-ai/xallor-remote/internal/releasetrust"
	"os"
	"path/filepath"
	"testing"
)

// A running baseline without its original proof material cannot be promised as
// a rollback target. Refusal must preserve the old process and install record.
func TestInstall_should_preserveCurrent_whenBaselineIsUnverifiable(t *testing.T) {
	for _, kind := range []string{"missing-manifest", "proof-denied", "manifest-mismatch"} {
		t.Run(kind, func(t *testing.T) {
			first, old := bundle(t, "old")
			second, next := bundle(t, "next")
			s := mechanicsStore(t)
			c := control(old, next)
			if err := s.Install(first, c); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(filepath.Join(s.Dir, "state.json"))
			if err != nil {
				t.Fatal(err)
			}
			beforeVersions := []string{}
			if entries, err := os.ReadDir(filepath.Join(s.Dir, "versions")); err == nil {
				for _, entry := range entries {
					beforeVersions = append(beforeVersions, entry.Name())
				}
			}
			live, stops, checks := c.live, c.stops, c.checks
			switch kind {
			case "missing-manifest":
				if err := os.Remove(filepath.Join(filepath.Dir(c.live.Path), "manifest.json")); err != nil {
					t.Fatal(err)
				}
			case "proof-denied":
				s.sourceVerifier = func(_ context.Context, _ string, id releasetrust.Identity, _, _ string) error {
					if id.Revision == old.Build.Revision {
						return releasetrust.ErrUnverified
					}
					return nil
				}
			case "manifest-mismatch":
				changed := old
				changed.Build.BuiltAt = "different"
				writeManifest(t, filepath.Dir(c.live.Path), changed)
			}
			if err := s.Install(second, c); err == nil {
				t.Fatal("upgrade accepted unverifiable baseline")
			}
			after, err := os.ReadFile(filepath.Join(s.Dir, "state.json"))
			if err != nil {
				t.Fatal(err)
			}
			if c.live != live || c.stops != stops || c.checks != checks || string(before) != string(after) {
				t.Fatal("baseline refusal changed process or state")
			}
			entries, err := os.ReadDir(filepath.Join(s.Dir, "versions"))
			if err != nil {
				t.Fatal(err)
			}
			afterVersions := []string{}
			for _, entry := range entries {
				afterVersions = append(afterVersions, entry.Name())
			}
			if fmt.Sprint(beforeVersions) != fmt.Sprint(afterVersions) {
				t.Fatal("baseline refusal staged candidate")
			}
		})
	}
}

// Stopped managed versions also need proof before they can be selected as the
// recovery baseline for a new installation.
func TestInstall_should_refuseStoppedBaseline_whenProofFails(t *testing.T) {
	first, old := bundle(t, "stopped-old")
	second, next := bundle(t, "next")
	s := mechanicsStore(t)
	c := control(old, next)
	if err := s.Install(first, c); err != nil {
		t.Fatal(err)
	}
	if err := c.Stop(); err != nil {
		t.Fatal(err)
	}
	stops, checks := c.stops, c.checks
	s.sourceVerifier = func(_ context.Context, _ string, id releasetrust.Identity, _, _ string) error {
		if id.Revision == old.Build.Revision {
			return releasetrust.ErrUnverified
		}
		return nil
	}
	if err := s.Install(second, c); !errors.Is(err, releasetrust.ErrUnverified) {
		t.Fatal("untrusted stopped baseline accepted", err)
	}
	if c.stops != stops || c.checks != checks || c.live != nil {
		t.Fatal("untrusted baseline reached activation")
	}
}

// A failed activation cannot implicitly re-authorize a previously valid version.
func TestRestore_should_notStartPrevious_whenItsProofIsRejected(t *testing.T) {
	first, old := bundle(t, "restore-old")
	s := mechanicsStore(t)
	c := control(old)
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
	proofs := 0
	pending := state
	pending.Pending = &old
	pending.PendingActivation = state.CurrentActivation
	pending.PendingLaunchID, err = activationID()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.save(pending); err != nil {
		t.Fatal(err)
	}
	s.sourceVerifier = func(context.Context, string, releasetrust.Identity, string, string) error {
		proofs++
		return releasetrust.ErrUnverified
	}
	err = s.restore(state, c, errors.New("candidate failed"))
	if !errors.Is(err, releasetrust.ErrUnverified) || proofs != 1 || c.live != nil {
		t.Fatal("recovery started an unverified baseline", err)
	}
}
