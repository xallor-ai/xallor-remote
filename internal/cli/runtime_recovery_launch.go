package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/release"
	"github.com/xallor-ai/xallor-remote/internal/runtimehead"
	"github.com/xallor-ai/xallor-remote/internal/runtimeowner"
)

// recoverLegacyPending is the explicit no-serving-owner bootstrap exception.
// The head authenticates one retained recovery image, never runs Rollback itself,
// and never selects another candidate after an unknown result.
func recoverLegacyPending(ctx context.Context, notice io.Writer) error {
	ctx, cancel := context.WithTimeout(ctx, 240*time.Second)
	defer cancel()
	s, err := runtimeStore()
	if err != nil {
		return err
	}
	command, err := runtimeowner.NewCommand("rollback", "")
	if err != nil {
		return err
	}
	request, _ := command.Request()
	journal := runtimeowner.Journal{Dir: filepath.Join(s.Dir, "lifecycle")}
	var permit release.OwnerHandoff
	var activation release.Activation
	err = journal.WithBootstrap(func() error {
		if err := confirmNoRecoveryOwner(); err != nil {
			return err
		}
		var err error
		permit, activation, err = s.NewRecoveryHandoff(ctx, os.Getpid(), request.ID, request.IntentSHA256)
		if err != nil {
			return err
		}
		probe, err := probeRuntimeIdentity(ctx, activation.Binary, permit.ImageSHA256)
		if err != nil {
			return err
		}
		if probe.Info != activation.Manifest.Build || probe.SHA256 != permit.ImageSHA256 || probe.OwnerProtocol != buildinfo.RuntimeOwnerProtocol {
			return errors.New("retained recovery image is incompatible")
		}
		return confirmNoRecoveryOwner()
	})
	if err != nil {
		return err
	}
	if notice != nil {
		if _, err := fmt.Fprintf(notice, "恢复事务：%s；可用 runtime status --operation %s 查询。\n", command.ID, command.ID); err != nil {
			return err
		}
	}
	attempt, launchErr := launchTransactionImage(ctx, command, permit, activation, "runtime-recovery", s.Dir)
	if attempt == nil {
		return launchErr
	}
	// The bootstrap head owns this exact child handle. Once it exits, reading
	// the correlated journal is an offline observation, not a second installer.
	// This also covers clearing a failed first bootstrap with no serving owner.
	select {
	case <-attempt.Done():
	case <-ctx.Done():
		return &runtimehead.OperationError{ID: command.ID, Cause: errors.Join(ctx.Err(), launchErr)}
	}
	processErr := attempt.Result()
	record, settled, queryErr := journal.QuerySettled(command.ID)
	if queryErr != nil {
		return &runtimehead.OperationError{ID: command.ID, Cause: errors.Join(queryErr, launchErr, processErr)}
	}
	if err := record.ValidateFor(request); err != nil {
		return &runtimehead.OperationError{ID: command.ID, Cause: err}
	}
	if !settled || record.Outcome == runtimeowner.Pending || record.Outcome == runtimeowner.Uncertain {
		return &runtimehead.OperationError{ID: command.ID, Cause: errors.Join(runtimeowner.ErrUncertain, processErr)}
	}
	if processErr != nil {
		return &runtimehead.OperationError{ID: command.ID, Cause: processErr}
	}
	if record.Outcome != runtimeowner.Succeeded {
		return fmt.Errorf("恢复事务 %s 已结束，结果为 %s。", command.ID, record.Outcome)
	}
	return nil
}
