package cli

import (
	"errors"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/release"
)

type runtimeStartResult struct {
	waitErr  error
	imageErr error
	exited   bool
}

func (r runtimeStartResult) cleanupError() error {
	if !r.exited || r.imageErr != nil {
		return errors.Join(release.ErrCleanupUncertain, r.waitErr, r.imageErr)
	}
	return nil
}

func stopOwnedRuntime(kill func() error, done <-chan runtimeStartResult, budget time.Duration) error {
	select {
	case result := <-done:
		return result.cleanupError()
	default:
	}
	killErr := kill()
	timer := time.NewTimer(budget)
	defer timer.Stop()
	select {
	case result := <-done:
		return result.cleanupError()
	case <-timer.C:
		return errors.Join(release.ErrCleanupUncertain, killErr, errors.New("owned Runtime exit was not confirmed before deadline"))
	}
}
