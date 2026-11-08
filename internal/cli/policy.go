package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

func cmdPolicy() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "policy",
		Short: "显示远程操作策略矩阵",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			matrix := protocol.PolicyMatrix()
			if asJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(matrix)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "version=%d sandbox=%s\n", matrix.Version, matrix.Sandbox)
			for _, row := range matrix.Operations {
				fmt.Fprintf(cmd.OutOrStdout(), "%s: %s; target=%s; terminal=%s; retry=%s\n", row.Operation, row.Authorization, row.TargetBoundary, row.Terminal, row.Retry)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "输出 JSON")
	return c
}
