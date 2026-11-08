package cli

import "github.com/spf13/cobra"

func cmdPeerRename() *cobra.Command {
	return cmdPeerRenameWith(ensureRuntime, rpc)
}

func cmdPeerRenameWith(ensure func() error, call peerRPC) *cobra.Command {
	var device, name string
	c := &cobra.Command{Use: "rename", Short: "修改设备备注", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := ensure(); err != nil {
				return err
			}
			_, err := call("peer.rename", map[string]any{"device_id": device, "name": name})
			return err
		}}
	c.Flags().StringVar(&device, "device", "", "对方设备 ID")
	c.Flags().StringVar(&name, "name", "", "设备备注；空值清除备注")
	_ = c.MarkFlagRequired("device")
	_ = c.MarkFlagRequired("name")
	return c
}
