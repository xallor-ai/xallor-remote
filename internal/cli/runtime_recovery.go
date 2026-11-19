package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/release"
	"github.com/xallor-ai/xallor-remote/internal/runtimeimage"
	"github.com/xallor-ai/xallor-remote/internal/runtimeowner"
	"github.com/xallor-ai/xallor-remote/internal/runtimeprocess"
)

func cmdRuntimeRecovery() *cobra.Command {
	return &cobra.Command{Use: "runtime-recovery", Hidden: true, Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error { return runRuntimeRecovery() }}
}

func confirmNoRecoveryOwner() error {
	live, err := (runtimeControl{}).Running()
	if err != nil {
		return err
	}
	if live != nil {
		return errors.New("a Runtime is already serving; cold recovery refused")
	}
	return confirmRuntimeUnlocked()
}

type recoveryAdmission struct {
	ctx    context.Context
	store  release.Store
	parent runtimeprocess.ExitWatch
	permit release.OwnerHandoff
}

func (a *recoveryAdmission) Check() error {
	if err := a.ctx.Err(); err != nil {
		return err
	}
	if exited, err := a.parent.Exited(); err != nil || exited {
		return errors.Join(errors.New("recovery launcher no longer available before admission"), err)
	}
	if err := a.store.CheckOwnerHandoffState(a.permit); err != nil {
		return err
	}
	return confirmNoRecoveryOwner()
}
func (a *recoveryAdmission) Close() error { return a.parent.Close() }

func runRuntimeRecovery() (result error) {
	info, err := os.Stdin.Stat()
	if err != nil || info.Mode()&os.ModeNamedPipe == 0 {
		return errors.New("recovery requires an inherited input pipe")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	parentPID := os.Getppid()
	parent, err := runtimeprocess.WatchExit(parentPID)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, parent.Close()) }()
	s, err := runtimeStore()
	if err != nil {
		return err
	}
	if err := confirmNoRecoveryOwner(); err != nil {
		return err
	}
	// Authenticate this already running bootstrap image before reading commands.
	// An arbitrary executable cannot use a recovery envelope as a launch license.
	activation, _, err := s.RecoveryActivation(ctx)
	if err != nil {
		return err
	}
	live := buildinfo.Running()
	if live.Info != activation.Manifest.Build || live.SHA256 != activation.Manifest.Artifact.SHA256 || live.OwnerProtocol != buildinfo.RuntimeOwnerProtocol || !runtimeimage.MatchesPath(live.Path, activation.Binary) {
		return errors.New("recovery process is not the retained authenticated image")
	}
	inputContext, stopInput := context.WithTimeout(ctx, 5*time.Second)
	payload, inputErr := runtimeowner.ReadHandoffPipe(inputContext, os.Stdin)
	stopInput()
	if inputErr != nil {
		return fmt.Errorf("recovery input not completed: %w", inputErr)
	}
	control := &recoveryControl{Control: runtimeControl{}, profile: filepath.Dir(s.Dir)}
	defer control.Close()
	worker := runtimeowner.Worker{Journal: runtimeowner.Journal{Dir: filepath.Join(s.Dir, "lifecycle")}, Store: s, Control: control, Listen: ipc.ListenLifecycle,
		Authenticate: func(ctx context.Context, h runtimeowner.Handoff) (runtimeowner.Admission, error) {
			if h.Command.Kind != "rollback" {
				return nil, errors.New("recovery accepts only explicit rollback")
			}
			r, err := h.Command.Request()
			if err != nil {
				return nil, err
			}
			if err := s.AuthorizeRecoveryHandoff(ctx, h.Permit, live, parentPID, r.ID, r.IntentSHA256); err != nil {
				return nil, err
			}
			return &recoveryAdmission{ctx: ctx, store: s, parent: parent, permit: h.Permit}, nil
		},
	}
	return worker.Run(ctx, bytes.NewReader(payload), transactionAcceptanceWriter{file: os.Stdout})
}
