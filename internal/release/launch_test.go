package release

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/releasetrust"
)

func launchFixture(t *testing.T) (Store, Activation, LaunchPermit, buildinfo.Executable) {
	t.Helper()
	s, a := stagedCleanupFixture(t)
	if err := s.save(State{Pending: &a.Manifest, PendingActivation: a.ID}); err != nil {
		t.Fatal(err)
	}
	permit, err := s.NewLaunchPermit(a.Binary, a.Manifest.Artifact.SHA256, 100)
	if err != nil {
		t.Fatal(err)
	}
	live := buildinfo.Executable{Info: a.Manifest.Build, Path: a.Binary, SHA256: a.Manifest.Artifact.SHA256, PID: 101, OwnerProtocol: buildinfo.RuntimeOwnerProtocol}
	return s, a, permit, live
}

// Inherited launch context only admits the exact verified image/profile/state.
// Accepted proof here is private test wiring, not signed-launch acceptance.
func TestAuthorizeLaunch_should_bindParentProfileSelectionAndActualImage(t *testing.T) {
	for _, kind := range []string{"valid", "parent", "profile", "state", "activation", "digest", "image", "owner", "proof", "selection-during-proof", "stale-after-selection"} {
		t.Run(kind, func(t *testing.T) {
			s, a, p, live := launchFixture(t)
			parent := 100
			switch kind {
			case "parent":
				parent = 102
			case "profile":
				p.ProfileSHA256 = strings.Repeat("a", 64)
			case "state":
				p.StateSHA256 = strings.Repeat("a", 64)
			case "activation":
				p.ActivationID = strings.Repeat("a", 32)
			case "digest":
				p.SHA256 = strings.Repeat("a", 64)
			case "image":
				live.Path = filepath.Join(t.TempDir(), a.Manifest.Artifact.Filename)
			case "owner":
				live.OwnerProtocol = 0
			case "proof":
				s.sourceVerifier = func(context.Context, string, releasetrust.Identity, string, string) error {
					return releasetrust.ErrUnverified
				}
			case "selection-during-proof":
				s.sourceVerifier = func(context.Context, string, releasetrust.Identity, string, string) error {
					return s.save(State{Current: &a.Manifest, CurrentActivation: a.ID})
				}
			case "stale-after-selection":
				if err := s.save(State{Current: &a.Manifest, CurrentActivation: a.ID}); err != nil {
					t.Fatal(err)
				}
			}
			err := s.AuthorizeLaunch(context.Background(), p, live, parent)
			if (err == nil) != (kind == "valid") {
				t.Fatalf("kind=%s err=%v", kind, err)
			}
			if _, err := os.Stat(filepath.Join(s.Dir, "identity.db")); !os.IsNotExist(err) {
				t.Fatal("admission initialized identity", err)
			}
		})
	}
}

// A stopped selected state is not a launch transition. The public start path
// must ask the owner controller to create pending rather than directly serve.
func TestNewLaunchPermit_should_refuseWithoutOwnerTransition(t *testing.T) {
	s, a := stagedCleanupFixture(t)
	if err := s.save(State{Current: &a.Manifest, CurrentActivation: a.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.NewLaunchPermit(a.Binary, a.Manifest.Artifact.SHA256, 100); err == nil {
		t.Fatal("uncontrolled launch admitted")
	}
}

// A checked launch cannot connect to Relay or accept business until the parent
// publishes this activation. A different selection must stop the waiting child.
func TestLaunchSelected_should_waitForMatchingCommit(t *testing.T) {
	s, a, p, live := launchFixture(t)
	if ready, err := s.LaunchSelected(p, live); ready || err != nil {
		t.Fatal("pending launch admitted", ready, err)
	}
	if err := s.save(State{Current: &a.Manifest, CurrentActivation: a.ID}); err != nil {
		t.Fatal(err)
	}
	if ready, err := s.LaunchSelected(p, live); !ready || err != nil {
		t.Fatal("matching commit not admitted", ready, err)
	}
	if err := s.save(State{}); err != nil {
		t.Fatal(err)
	}
	if ready, err := s.LaunchSelected(p, live); ready || err == nil {
		t.Fatal("superseded launch admitted", ready, err)
	}
}
