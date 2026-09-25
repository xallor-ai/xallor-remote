package cli

import (
	"github.com/spf13/cobra"

	"github.com/xallor-ai/xallor-remote/internal/tui"
)

func cmdTUI() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "本机交互界面",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := ensureRuntime(); err != nil {
				return err
			}
			return tui.Run()
		},
	}
}
