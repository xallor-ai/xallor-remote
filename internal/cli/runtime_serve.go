package cli

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/identity"
	"github.com/xallor-ai/xallor-remote/internal/noderun"
	"github.com/xallor-ai/xallor-remote/internal/release"
	"github.com/xallor-ai/xallor-remote/internal/runtimeowner"
	"github.com/xallor-ai/xallor-remote/internal/runtimeprocess"
)

func cmdRuntimeServe() *cobra.Command {
	return &cobra.Command{Use: "runtime-serve", Hidden: true, Args: cobra.NoArgs, RunE: func(_ *cobra.Command, _ []string) error {
		input, err := os.Stdin.Stat()
		if err != nil || input.Mode()&os.ModeNamedPipe == 0 {
			return errors.New("Runtime service requires an inherited launch pipe")
		}
		inputContext, stopInput := context.WithTimeout(context.Background(), 5*time.Second)
		raw, err := runtimeowner.ReadHandoffPipe(inputContext, os.Stdin)
		stopInput()
		if err != nil || len(raw) > 1024 {
			return errors.Join(errors.New("invalid Runtime launch context"), err)
		}
		var permit release.LaunchPermit
		if err := json.Unmarshal(raw, &permit); err != nil {
			return errors.New("invalid Runtime launch context")
		}
		if permit.ParentPID != os.Getppid() || permit.ParentPID <= 0 {
			return errors.New("Runtime launch parent changed")
		}
		parent, err := runtimeprocess.WatchExit(permit.ParentPID)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		done := make(chan struct{})
		go func() { defer close(done); _ = parent.Wait(ctx); cancel() }()
		var gate *launchCommitGate
		defer func() {
			cancel()
			<-done
			// Daemon shutdown may race its startup polling goroutine. Serialize
			// handle release with gate observation; a late check must not use
			// a closed/recycled process handle.
			if gate != nil {
				_ = gate.Close()
			} else {
				_ = parent.Close()
			}
		}()
		s, err := runtimeStore()
		if err != nil {
			return err
		}
		if err := s.AuthorizeLaunch(ctx, permit, buildinfo.Running(), os.Getppid()); err != nil {
			return err
		}
		// Stop the admission wait before sharing this observer with the commit
		// gate. Keep its kernel handle alive until selection or service exit.
		cancel()
		<-done
		commitDeadline := time.Now().Add(90 * time.Second)
		selected := func() (bool, error) { return s.LaunchSelected(permit, buildinfo.Running()) }
		gate = &launchCommitGate{selected: selected, exited: parent.Exited, closeObserver: parent.Close, deadline: commitDeadline}
		return runRuntimeService(gate.Check, permit.LaunchID, permit.ActivationID)
	}}
}

func runRuntimeService(gate func() (bool, error), launchID string, activationID string) error {
	unlock, err := lockRuntime()
	if err != nil {
		return err
	}
	defer unlock()
	if gate != nil {
		if _, err := gate(); err != nil {
			return err
		}
	}
	st, err := identity.Load()
	if err != nil {
		return err
	}
	printBanner(st)
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	daemon := noderun.New(st, log)
	daemon.WithLaunchIdentity(launchID)
	daemon.WithActivationIdentity(activationID)
	daemon.WithLifecycleLauncher(func(ctx context.Context, command runtimeowner.Command) (runtimeowner.Attempt, error) {
		return launchRuntimeTransaction(ctx, command, launchID)
	})
	if gate != nil {
		daemon.WithStartupGate(gate)
	}
	return daemon.Run()
}
