package cli

import (
	"encoding/json"
	"time"

	"github.com/spf13/cobra"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

func cmdDevice() *cobra.Command {
	root := &cobra.Command{Use: "device", Short: "本机设备归属与分享"}
	root.AddCommand(&cobra.Command{Use: "lifecycle", Short: "显示设备 enrollment/claim/transfer/revoke/reset 契约", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(protocol.DeviceLifecycle())
	}})
	run := func(cmd *cobra.Command, parameters map[string]any) error {
		if err := ensureRuntime(); err != nil {
			return err
		}
		state, err := rpc("device.access", parameters)
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(state)
	}
	for _, item := range []struct{ command, action, help string }{{"claim", "claim", "将本机设备认领到当前身份的空间"}, {"owner", "status", "查看本机设备归属和分享"}} {
		root.AddCommand(&cobra.Command{Use: item.command, Short: item.help, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error { return run(cmd, map[string]any{"action": item.action}) }})
	}
	var transferMember string
	transfer := &cobra.Command{Use: "transfer", Short: "转移本机设备归属并作废原分享", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return run(cmd, map[string]any{"action": "transfer", "membership_id": transferMember})
	}}
	transfer.Flags().StringVar(&transferMember, "member", "", "接收者的成员关系 ID")
	_ = transfer.MarkFlagRequired("member")
	var member string
	var operations []string
	var ttl time.Duration
	share := &cobra.Command{Use: "share", Short: "向指定成员授权本机设备操作", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return run(cmd, map[string]any{"action": "share", "membership_id": member, "operations": operations, "ttl_seconds": int64(ttl / time.Second)})
	}}
	share.Flags().StringVar(&member, "member", "", "接收者的成员关系 ID")
	share.Flags().StringSliceVar(&operations, "allow", nil, "允许的操作，以逗号分隔")
	share.Flags().DurationVar(&ttl, "ttl", 24*time.Hour, "授权有效期")
	_ = share.MarkFlagRequired("member")
	_ = share.MarkFlagRequired("allow")
	var shareID string
	unshare := &cobra.Command{Use: "unshare", Short: "撤销指定分享", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return run(cmd, map[string]any{"action": "unshare", "share_id": shareID})
	}}
	unshare.Flags().StringVar(&shareID, "share", "", "分享 ID")
	_ = unshare.MarkFlagRequired("share")
	root.AddCommand(transfer, share, unshare)
	return root
}
