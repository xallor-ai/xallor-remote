package release

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/releasetrust"
	"github.com/xallor-ai/xallor-remote/internal/runtimeprocess"
)

// The table tests binding/provenance with synthetic identities; the separate
// real-process regression below never replaces kernel image observation.
type handoffTestImage struct{}

func (handoffTestImage) Exited() (bool, error)      { return false, nil }
func (handoffTestImage) Wait(context.Context) error { return nil }
func (handoffTestImage) Close() error               { return nil }

// The handoff binds selected provenance, actual worker image, OS-observed parent
// and IPC peer, profile, stable state and the exact operation intent.
func TestOwnerHandoffRefusesChangedAuthorityBeforeMutation(t *testing.T) {
	for _, kind := range []string{"valid", "parent", "peer", "worker-image", "worker-build", "worker-path", "worker-protocol", "operation", "intent", "profile", "state", "activation", "owner-launch", "proof", "state-during-proof", "pending"} {
		t.Run(kind, func(t *testing.T) {
			s, a := stagedCleanupFixture(t)
			s.handoffImageVerifier = func(int, string, string) (runtimeprocess.ExitWatch, error) { return handoffTestImage{}, nil }
			if err := s.save(State{Current: &a.Manifest, CurrentActivation: a.ID, CurrentLaunchID: strings.Repeat("4", 32)}); err != nil {
				t.Fatal(err)
			}
			parent := buildinfo.Executable{Info: a.Manifest.Build, Path: a.Binary, SHA256: a.Manifest.Artifact.SHA256, PID: 100, OwnerProtocol: buildinfo.RuntimeOwnerProtocol}
			id, intent := strings.Repeat("1", 32), strings.Repeat("a", 64)
			permit, _, err := s.NewOwnerHandoff(context.Background(), parent, id, intent)
			if err != nil {
				t.Fatal(err)
			}
			worker := parent
			worker.PID = 101
			actualParent, peer := 100, 100
			switch kind {
			case "owner-launch":
				permit.OwnerLaunchID = strings.Repeat("5", 32)
			case "parent":
				actualParent = 102
			case "peer":
				peer = 102
			case "worker-image":
				worker.SHA256 = strings.Repeat("b", 64)
			case "worker-build":
				worker.Version = "0.0.1"
			case "worker-path":
				worker.Path = filepath.Join(t.TempDir(), "other")
			case "worker-protocol":
				worker.OwnerProtocol = 0
			case "operation":
				id = strings.Repeat("2", 32)
			case "intent":
				intent = strings.Repeat("b", 64)
			case "profile":
				permit.ProfileSHA256 = strings.Repeat("b", 64)
			case "state":
				permit.StateSHA256 = strings.Repeat("b", 64)
			case "activation":
				permit.ActivationID = strings.Repeat("b", 32)
			case "proof":
				s.sourceVerifier = func(context.Context, string, releasetrust.Identity, string, string) error {
					return releasetrust.ErrUnverified
				}
			case "state-during-proof":
				s.sourceVerifier = func(context.Context, string, releasetrust.Identity, string, string) error { return s.save(State{}) }
			case "pending":
				if err := s.save(State{Current: &a.Manifest, CurrentActivation: a.ID, CurrentLaunchID: strings.Repeat("4", 32), Pending: &a.Manifest, PendingActivation: a.ID}); err != nil {
					t.Fatal(err)
				}
			}
			err = s.AuthorizeOwnerHandoff(context.Background(), permit, worker, parent, actualParent, peer, id, intent)
			if (err == nil) != (kind == "valid") {
				t.Fatal(kind, err)
			}
			if _, err := os.Stat(filepath.Join(s.Dir, "lifecycle")); !os.IsNotExist(err) {
				t.Fatal("authentication wrote journal", err)
			}
		})
	}
}

// Creating an envelope cannot convert a pending/unselected or unproven image
// into an owner. No alternative activation is selected after refusal.
func TestOwnerHandoffRequiresReadyOwnerAndFreshProof(t *testing.T) {
	s, a := stagedCleanupFixture(t)
	owner := buildinfo.Executable{Info: a.Manifest.Build, Path: a.Binary, SHA256: a.Manifest.Artifact.SHA256, PID: 100, OwnerProtocol: buildinfo.RuntimeOwnerProtocol}
	id, intent := strings.Repeat("1", 32), strings.Repeat("a", 64)
	if _, _, err := s.NewOwnerHandoff(context.Background(), owner, id, intent); err == nil {
		t.Fatal("unselected owner accepted")
	}
	if err := s.save(State{Current: &a.Manifest, CurrentActivation: a.ID, CurrentLaunchID: strings.Repeat("4", 32)}); err != nil {
		t.Fatal(err)
	}
	s.sourceVerifier = func(context.Context, string, releasetrust.Identity, string, string) error {
		return releasetrust.ErrUnverified
	}
	if _, _, err := s.NewOwnerHandoff(context.Background(), owner, id, intent); err == nil {
		t.Fatal("unverified owner accepted")
	}
}

// Matching OS-parent/peer/PID numbers cannot authenticate self-reported image
// fields: this real test process is not the fixture's selected Runtime image.
func TestOwnerHandoffRefusesNonRuntimeParentDespiteMatchingPIDs(t *testing.T) {
	s, a := stagedCleanupFixture(t)
	if err := s.save(State{Current: &a.Manifest, CurrentActivation: a.ID, CurrentLaunchID: strings.Repeat("4", 32)}); err != nil {
		t.Fatal(err)
	}
	parent := buildinfo.Executable{Info: a.Manifest.Build, Path: a.Binary, SHA256: a.Manifest.Artifact.SHA256, PID: os.Getpid(), OwnerProtocol: buildinfo.RuntimeOwnerProtocol}
	id, intent := strings.Repeat("1", 32), strings.Repeat("a", 64)
	permit, _, err := s.NewOwnerHandoff(context.Background(), parent, id, intent)
	if err != nil {
		t.Fatal(err)
	}
	worker := parent
	worker.PID++
	if err := s.AuthorizeOwnerHandoff(context.Background(), permit, worker, parent, parent.PID, parent.PID, id, intent); err == nil {
		t.Fatal("non-Runtime parent accepted using claimed image fields")
	}
}
