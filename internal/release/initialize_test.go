package release

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/releasetrust"
)

// Fresh installation is the one bootstrap exception: it starts only the
// authenticated requested image and confirms the actual selected owner.
func TestInitializeOwnerStartsRequestedImageWithoutStoppingAnyOwner(t *testing.T) {
	dir, m := bundle(t, "fresh-owner")
	s := mechanicsStore(t)
	c := control(m)
	if err := s.InitializeOwner(dir, c); err != nil {
		t.Fatal(err)
	}
	state, err := s.Read()
	if err != nil || state.Current == nil || *state.Current != m || state.Pending != nil || c.live == nil || c.live.SHA256 != m.Artifact.SHA256 || c.stops != 0 {
		t.Fatal(state, c.live, c.stops, err)
	}
}

// An initialization race cannot turn a newly requested bundle into apparent
// success by merely observing another selected image under the lifecycle lock.
func TestInitializeOwnerRefusesExistingOrConcurrentlySelectedVersion(t *testing.T) {
	first, old := bundle(t, "existing-initialize")
	second, next := bundle(t, "requested-initialize")
	for _, concurrent := range []bool{false, true} {
		s := mechanicsStore(t)
		c := control(old, next)
		install := func() {
			if err := s.Install(first, c); err != nil {
				t.Fatal(err)
			}
		}
		if concurrent {
			s = s.WithBootstrapGuard(func(run func() error) error {
				unwrapped := s
				unwrapped.bootstrapGuard = nil
				if err := unwrapped.Install(first, c); err != nil {
					t.Fatal(err)
				}
				return run()
			})
		} else {
			install()
		}
		if err := s.InitializeOwner(second, c); err == nil {
			t.Fatal("initialization accepted another owner")
		}
		state, err := s.Read()
		if err != nil || state.Current == nil || *state.Current != old || c.live.SHA256 != old.Artifact.SHA256 {
			t.Fatal(state, err)
		}
	}
}

// No bootstrap directory, version probe or process effect precedes source proof.
func TestInitializeOwnerSourceDenialDoesNotCreateProfile(t *testing.T) {
	dir, m := bundle(t, "denied-initialize")
	s := Store{Dir: filepath.Join(t.TempDir(), "profile", "runtime"), sourceVerifier: func(context.Context, string, releasetrust.Identity, string, string) error {
		return releasetrust.ErrUnverified
	}}
	c := control(m)
	if err := s.InitializeOwner(dir, c); !errors.Is(err, releasetrust.ErrUnverified) {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.Dir); !os.IsNotExist(err) || c.checks != 0 || c.stops != 0 || c.live != nil {
		t.Fatal("source refusal had profile/process effects", err)
	}
}
