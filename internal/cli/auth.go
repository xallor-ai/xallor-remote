package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func cmdInbound() *cobra.Command {
	c := &cobra.Command{Use: "inbound", Short: "开或关入站"}
	c.AddCommand(&cobra.Command{
		Use:   "on",
		Short: "允许对方连入（还没有授权码时会先签发）",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := ensureRuntime(); err != nil {
				return err
			}
			if _, err := rpc("inbound.set", map[string]any{"enabled": true}); err != nil {
				return err
			}
			fmt.Println("入站已开。")
			return nil
		},
	})
	c.AddCommand(&cobra.Command{
		Use:   "off",
		Short: "关闭入站，身份保留",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := ensureRuntime(); err != nil {
				return err
			}
			if _, err := rpc("inbound.set", map[string]any{"enabled": false}); err != nil {
				return err
			}
			fmt.Println("入站已关。")
			return nil
		},
	})
	return c
}

func cmdRevoke() *cobra.Command {
	return &cobra.Command{
		Use:   "revoke",
		Short: "注销这台设备在中转上的身份",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := ensureRuntime(); err != nil {
				return err
			}
			if _, err := rpc("revoke", map[string]any{}); err != nil {
				return err
			}
			fmt.Println("已注销这台设备在中转上的身份。")
			return nil
		},
	}
}

func cmdReset() *cobra.Command {
	var yes bool
	c := &cobra.Command{
		Use:   "reset",
		Short: "清空本机身份并注销中转登记",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				return fmt.Errorf("reset 会注销这台设备在中转上的身份。确认请加 --yes")
			}
			if err := ensureRuntime(); err != nil {
				return err
			}
			if _, err := rpc("reset", map[string]any{"confirm": true}); err != nil {
				return err
			}
			fmt.Println("已注销本机身份。下次 start 会按登录用户名重新登记。")
			return nil
		},
	}
	c.Flags().BoolVar(&yes, "yes", false, "确认")
	return c
}

func cmdStop() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "停止本机 Runtime",
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := rpc("stop", map[string]any{}); err != nil {
				return err
			}
			fmt.Println("已停止。")
			return nil
		},
	}
}
