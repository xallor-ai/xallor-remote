package release

import (
	"context"
	"errors"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
)

var errOldRecoveryPermit = errors.New("old launch permit remained valid during recovery of the same activation")

type recoveryTransitionControl struct {
	*fakeControl
	store     Store
	old       LaunchPermit
	liveImage buildinfo.Executable
	checked   bool
}

func (c *recoveryTransitionControl) Start(path, sha string) error {
	c.checked = true
	if err := c.store.AuthorizeLaunch(context.Background(), c.old, c.liveImage, c.old.ParentPID); err == nil {
		return errOldRecoveryPermit
	}
	return c.fakeControl.Start(path, sha)
}

// Recovery may target the same activation as an interrupted restart. It still
// must revoke that old launch permit before releasing the service mutex.
func TestRecoveryRevokesOldPermitWhenTargetActivationIsUnchanged(t *testing.T) {
	s, a := stagedCleanupFixture(t)
	if err := s.save(State{Current: &a.Manifest, CurrentActivation: a.ID, Pending: &a.Manifest, PendingActivation: a.ID}); err != nil {
		t.Fatal(err)
	}
	permit, err := s.NewLaunchPermit(a.Binary, a.Manifest.Artifact.SHA256, 100)
	if err != nil {
		t.Fatal(err)
	}
	c := &recoveryTransitionControl{fakeControl: control(a.Manifest), store: s, old: permit, liveImage: buildinfo.Executable{
		Info: a.Manifest.Build, Path: a.Binary, SHA256: a.Manifest.Artifact.SHA256, PID: 101, OwnerProtocol: buildinfo.RuntimeOwnerProtocol}}
	if err := s.Rollback(c); err != nil {
		t.Fatal(err)
	}
	if !c.checked {
		t.Fatal("recovery never checked old permit revocation")
	}
	if ready, err := s.LaunchSelected(permit, c.liveImage); ready || err == nil {
		t.Fatal("old permit revived after committing same activation", ready, err)
	}
}
