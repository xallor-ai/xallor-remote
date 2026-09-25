package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/xallor-ai/xallor-remote/internal/mcpconfig"
)

func cmdMCP() *cobra.Command {
	c := &cobra.Command{Use: "mcp", Short: "MCP 配置"}
	c.AddCommand(&cobra.Command{
		Use:   "print-config",
		Short: "打印可粘贴到 Cursor 的 mcp.json 片段",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Print(`{
  "mcpServers": {
    "xallor-remote": {
      "command": "xallor-remote-mcp",
      "env": {
        "XALLOR_REMOTE_DEVICE_ID": "dev_对方设备ID",
        "XALLOR_REMOTE_DEVICE_GRANT": "xr_grant_对方授权码"
      }
    }
  }
}
`)
			fmt.Println("先 npm install -g xallor-remote-mcp（或本仓库打出的 tgz）。包未上公网 npm 前不要用 npx -y。授权码放环境变量，不要写进 git。")
			return nil
		},
	})
	var path, deviceID, grant string
	merge := &cobra.Command{
		Use:   "merge-config",
		Short: "幂等写入 Cursor mcp.json 中的本产品条目",
		RunE: func(cmd *cobra.Command, args []string) error {
			if path == "" {
				p, err := mcpconfig.DefaultPath()
				if err != nil {
					return err
				}
				path = p
			}
			r, err := mcpconfig.Merge(path, mcpconfig.Options{
				DeviceID: deviceID,
				Grant:    grant,
			})
			if err != nil {
				return err
			}
			if !r.Changed {
				fmt.Printf("已有 %s，未改动。\n", mcpconfig.ServerKey)
				fmt.Println(r.Path)
				return nil
			}
			fmt.Println("已写入", mcpconfig.ServerKey)
			fmt.Println(r.Path)
			fmt.Println("把 DEVICE_ID / GRANT 改成对方的值后重启 Cursor。")
			return nil
		},
	}
	merge.Flags().StringVar(&path, "path", "", "mcp.json 路径（默认 ~/.cursor/mcp.json）")
	merge.Flags().StringVar(&deviceID, "device-id", "", "写入的对方设备 ID")
	merge.Flags().StringVar(&grant, "grant", "", "写入的对方授权码")
	c.AddCommand(merge)
	return c
}
