package cli

import (
	"errors"
	"io"

	"github.com/spf13/cobra"
	"github.com/xallor-ai/xallor-remote/internal/release"
)

type foregroundRuntime struct {
	stdout, stderr io.Writer
	done           <-chan runtimeStartResult
}

func cmdStart() *cobra.Command {
	return cmdStartWith(ensureRuntimeWithControl)
}

func cmdStartWith(ensure func(release.Control) error) *cobra.Command {
	return &cobra.Command{Use: "start", Short: "前台运行受信 Runtime", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			foreground := &foregroundRuntime{stdout: cmd.OutOrStdout(), stderr: cmd.ErrOrStderr()}
			if err := ensure(runtimeControl{foreground: foreground}); err != nil {
				return err
			}
			if foreground.done == nil {
				return printStatus()
			} // Existing owner is reused.
			result := <-foreground.done
			return errors.Join(result.waitErr, result.cleanupError())
		},
	}
}
