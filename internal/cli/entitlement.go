package cli

import (
	"encoding/json"
	"github.com/spf13/cobra"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

func cmdEntitlement() *cobra.Command {
	root := &cobra.Command{Use: "entitlement", Short: "显示会员权益契约（不读取当前账单状态）"}
	root.AddCommand(&cobra.Command{Use: "contract", Short: "显示权益 feature/value 范围", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(protocol.EntitlementContract())
	}})
	return root
}
