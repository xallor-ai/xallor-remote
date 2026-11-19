package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/xallor-ai/xallor-remote/internal/appdata"
	"github.com/xallor-ai/xallor-remote/internal/filelock"
	"github.com/xallor-ai/xallor-remote/internal/job"
	"github.com/xallor-ai/xallor-remote/internal/release"
)

func (c runtimeControl) Stop() (result error) {
	attempt, err := c.PrepareStop(context.Background(), release.StopRequest{AbsencePolicy: "public_observation"})
	if attempt == nil {
		return errors.Join(err, release.ErrCleanupUncertain)
	}
	defer func() {
		if closures, err := attempt.Close(); release.StopCleanup(release.ResourceObligation{}, closures, err) != nil {
			result = errors.Join(result, release.StopCleanup(release.ResourceObligation{}, closures, err))
		}
	}()
	if err != nil {
		return err
	}
	_, err = attempt.Run(context.Background())
	return err
}

// The Runtime lifetime lock spans startup and shutdown beyond the IPC listener.
// Do not create an absent profile merely to observe that it has no owner.
func confirmRuntimeUnlocked() error {
	dir, err := appdata.DataDir()
	if err != nil {
		return err
	}
	info, err := os.Stat(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil || !info.IsDir() {
		return fmt.Errorf("%w: Runtime profile is not observable", release.ErrCleanupUncertain)
	}
	unlock, err := filelock.Acquire(filepath.Join(dir, "runtime.lock"))
	if err != nil {
		return fmt.Errorf("%w: IPC absent but Runtime lifetime lock unavailable: %v", release.ErrCleanupUncertain, err)
	}
	defer unlock()
	if err := job.CheckReleased(dir); err != nil {
		return jobCleanupError(err)
	}
	return nil
}
