package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/launcherresource"
	"github.com/xallor-ai/xallor-remote/internal/release"
	"github.com/xallor-ai/xallor-remote/internal/runtimeimage"
	"github.com/xallor-ai/xallor-remote/internal/runtimeowner"
)

// launchRuntimeTransaction is called by the serving owner. After Start, the
// attempt is returned even when acknowledgement is unknown. The owner must keep
// its launch gate until Done; timeout never authorizes another launch or kill.
func launchRuntimeTransaction(ctx context.Context, command runtimeowner.Command, ownerLaunchID string) (attempt runtimeowner.Attempt, result error) {
	request, err := command.Request()
	if err != nil {
		return nil, err
	}
	s, err := runtimeStore()
	if err != nil {
		return nil, err
	}
	journal := runtimeowner.Journal{Dir: filepath.Join(s.Dir, "lifecycle")}
	if existing, err := journal.Query(request.ID); err == nil {
		if existing.Request != request {
			return nil, runtimeowner.ErrConflict
		}
		return nil, runtimeowner.ErrRecorded
	} else if !errors.Is(err, runtimeowner.ErrUnknown) {
		return nil, err
	}
	if err := journal.RefuseBootstrap(); err != nil {
		return nil, err
	}
	permit, activation, err := s.NewOwnerHandoff(ctx, buildinfo.Running(), request.ID, request.IntentSHA256)
	if err != nil {
		return nil, err
	}
	if permit.OwnerLaunchID != ownerLaunchID {
		return nil, errors.New("serving Runtime launch identity changed")
	}
	return launchTransactionImage(ctx, command, permit, activation, "runtime-transaction", s.Dir)
}

func launchTransactionImage(ctx context.Context, command runtimeowner.Command, permit release.OwnerHandoff, activation release.Activation, entry, dir string) (attempt runtimeowner.Attempt, result error) {
	request, err := command.Request()
	if err != nil {
		return nil, err
	}
	payload, err := runtimeowner.EncodeHandoff(runtimeowner.Handoff{Command: command, Permit: permit})
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(activation.Binary, guardedArgs(entry)...)
	configureDetached(cmd)
	image, err := runtimeimage.Prepare(cmd, permit.ImageSHA256)
	if err != nil {
		return nil, runtimeImageAdmissionError(err)
	}
	started := false
	resources := []*launcherresource.Owned{launcherresource.Own(image)}
	defer func() {
		if !started {
			result = errors.Join(result, launcherresource.CloseAll(resources...))
		}
	}()
	output, writer, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	ownedOutput, ownedWriter := launcherresource.Own(output), launcherresource.Own(writer)
	resources = append(resources, ownedOutput, ownedWriter)
	log, err := os.OpenFile(filepath.Join(dir, "transaction.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	ownedLog := launcherresource.Own(log)
	resources = append(resources, ownedLog)
	cmd.Stdin = bytes.NewReader(payload)
	cmd.Stdout = writer
	cmd.Stderr = log
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	started = true
	// The child owns its inherited handles. Cache each parent-side close result
	// now; the attempt includes these same results when it finally settles.
	earlyCloseErr := launcherresource.CloseAll(ownedWriter, ownedLog)
	owned, acknowledged := launcherresource.Start(ctx, cmd.Wait, func(readCtx context.Context) error {
		defer ownedOutput.Close()
		reader, err := launcherresource.NewPipeReader(readCtx, output)
		if err != nil {
			return err
		}
		var ack runtimeowner.Acceptance
		decoder := json.NewDecoder(io.LimitReader(reader, 4097))
		decoder.DisallowUnknownFields()
		err = decoder.Decode(&ack)
		if err == nil && (!ack.Accepted || ack.OperationID != request.ID || ack.IntentSHA256 != request.IntentSHA256 || ack.PID != cmd.Process.Pid) {
			err = errors.New("worker acceptance does not match the owned request/process")
		}
		return err
	}, resources...)
	attempt = owned
	select {
	case err := <-acknowledged:
		if err != nil {
			return attempt, errors.Join(earlyCloseErr, fmt.Errorf("%w: lifecycle acceptance not confirmed; query operation %s: %v", release.ErrCleanupUncertain, request.ID, err))
		}
		return attempt, earlyCloseErr
	case <-ctx.Done():
		return attempt, errors.Join(earlyCloseErr, fmt.Errorf("%w: lifecycle acceptance deadline; query operation %s", release.ErrCleanupUncertain, request.ID))
	}
}
