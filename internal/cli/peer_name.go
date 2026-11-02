package cli

import "github.com/spf13/cobra"

func cmdPeerRename() *cobra.Command {
	var id, name string
	c := &cobra.Command{Use: "rename", Short: "修改设备备注", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := ensureRuntime(); err != nil {
				return err
			}
			_, err := rpc("peer.rename", map[string]any{"device_id": id, "name": name})
			return err
		}}
	c.Flags().StringVar(&id, "id", "", "对方设备 ID")
	c.Flags().StringVar(&name, "name", "", "设备备注；空值清除备注")
	_ = c.MarkFlagRequired("id")
	_ = c.MarkFlagRequired("name")
	return c
}
