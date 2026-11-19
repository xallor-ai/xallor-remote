package cli

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/xallor-ai/xallor-remote/internal/filelock"
	"github.com/xallor-ai/xallor-remote/internal/job"
	"github.com/xallor-ai/xallor-remote/internal/release"
)

// Recovery retains the service lock after confirmed Stop until it has revoked
// the old pending launch (clear or new pending selection). A delayed old child
// must recheck its admission under this same lock before identity/IPC effects.
type recoveryControl struct {
	release.Control
	profile string
	unlock  func()
}

func (c *recoveryControl) PrepareStop(ctx context.Context, request release.StopRequest) (release.StopAttempt, error) {
	request.ObserveResources = c.observeResources
	return c.Control.PrepareStop(ctx, request)
}
func (c *recoveryControl) observeResources() (string, string, error) {
	if c.unlock == nil {
		unlock, err := filelock.Acquire(filepath.Join(c.profile, "runtime.lock"))
		if err != nil {
			return "unknown", "unreleased_or_unknown", errors.Join(release.ErrCleanupUncertain, err)
		}
		c.unlock = unlock
	}
	jobs, err := job.ObserveReleased(c.profile)
	return "held_by_recovery", jobs, jobCleanupError(err)
}

func (c *recoveryControl) Start(path, sha string) error {
	// release.activate publishes the replacement pending selection before
	// Start. Its old launch permit is invalid before this mutex is released.
	c.Close()
	return c.Control.Start(path, sha)
}

func (c *recoveryControl) Close() {
	if c.unlock != nil {
		c.unlock()
		c.unlock = nil
	}
}
