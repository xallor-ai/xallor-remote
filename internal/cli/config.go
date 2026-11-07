package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/xallor-ai/xallor-remote/internal/shell"
)

func cmdConfig() *cobra.Command {
	c := &cobra.Command{Use: "config", Short: "本机配置"}
	c.AddCommand(&cobra.Command{Use: "get", Short: "显示本机配置", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if err := ensureRuntime(); err != nil {
			return err
		}
		result, err := rpc("config.get", nil)
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
	}})
	var name string
	set := &cobra.Command{Use: "set", Short: "设置本机命令解释器", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if name == "" {
			return fmt.Errorf("需要 --shell；自动选择请填 auto。")
		}
		if err := shell.Validate(name); err != nil {
			return fmt.Errorf("本系统不支持此 Shell。")
		}
		if err := ensureRuntime(); err != nil {
			return err
		}
		result, err := rpc("config.set", map[string]any{"shell": name})
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
	}}
	set.Flags().StringVar(&name, "shell", "", "Shell 名称；auto 使用系统默认")
	c.AddCommand(set)
	return c
}
