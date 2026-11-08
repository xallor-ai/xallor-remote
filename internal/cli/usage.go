package cli

import (
	"encoding/json"
	"github.com/spf13/cobra"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

func cmdUsage() *cobra.Command {
	root := &cobra.Command{Use: "usage", Short: "显示隐私最小化 usage event 契约"}
	root.AddCommand(&cobra.Command{Use: "contract", Short: "显示 usage 字段与 unknown 规则", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(protocol.UsageEventContract())
	}})
	return root
}
