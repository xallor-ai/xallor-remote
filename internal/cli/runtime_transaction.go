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
	"github.com/xallor-ai/xallor-remote/internal/runtimeowner"
)

func cmdRuntimeTransaction() *cobra.Command {
	return &cobra.Command{Use: "runtime-transaction", Hidden: true, Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error { return runRuntimeTransaction() }}
}

func runRuntimeTransaction() (result error) {
	input, err := os.Stdin.Stat()
	if err != nil || input.Mode()&os.ModeNamedPipe == 0 {
		return errors.New("lifecycle worker requires inherited input")
	}
	s, err := runtimeStore()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	worker := buildinfo.Running()
	admission, err := openTransactionParent(ctx, s, worker, os.Getppid())
	if err != nil {
		return err
	}
	// No external data is read before the parent/IPC/image relationship is
	// established. The trusted launcher supplies one finite bytes.Reader pipe.
	defer func() { result = errors.Join(result, admission.Close()) }()
	inputContext, stopInput := context.WithTimeout(ctx, 5*time.Second)
	payload, inputErr := runtimeowner.ReadHandoffPipe(inputContext, os.Stdin)
	stopInput()
	if inputErr != nil {
		return fmt.Errorf("transaction input not completed: %w", inputErr)
	}
	operation := runtimeowner.Worker{
		Journal: runtimeowner.Journal{Dir: filepath.Join(s.Dir, "lifecycle")}, Store: s, Control: runtimeControl{}, Listen: ipc.ListenLifecycle,
		Authenticate: func(ctx context.Context, handoff runtimeowner.Handoff) (runtimeowner.Admission, error) {
			request, err := handoff.Command.Request()
			if err != nil {
				return nil, err
			}
			monitor, stopMonitor := context.WithCancel(ctx)
			done := make(chan struct{})
			go func() {
				defer close(done)
				_ = admission.parent.Wait(monitor)
				if monitor.Err() == nil {
					cancel()
				}
			}()
			err = s.AuthorizeOwnerHandoff(ctx, handoff.Permit, worker, admission.owner, admission.parentPID, admission.peerPID, request.ID, request.IntentSHA256)
			stopMonitor()
			<-done // no concurrent Wait/Exited/Close on the observer
			if err != nil {
				return nil, err
			}
			admission.permit = handoff.Permit
			admission.authorized = true
			return admission, nil
		},
	}
	return operation.Run(ctx, bytes.NewReader(payload), transactionAcceptanceWriter{file: os.Stdout})
}
