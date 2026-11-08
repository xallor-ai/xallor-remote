package cli

import (
	"encoding/json"
	"fmt"
	"github.com/spf13/cobra"
	"github.com/xallor-ai/xallor-remote/internal/appdata"
	"github.com/xallor-ai/xallor-remote/internal/filelock"
	"github.com/xallor-ai/xallor-remote/internal/release"
	"os"
	"path/filepath"
)

func runtimeStore() (release.Store, error) {
	dir, err := appdata.DataDir()
	return release.Store{Dir: filepath.Join(dir, "runtime")}, err
}
func cmdRuntime() *cobra.Command {
	root := &cobra.Command{Use: "runtime", Short: "验证来源、安装、查看或回滚本机版本"}
	root.AddCommand(cmdRuntimeVerify())
	var bundle string
	install := &cobra.Command{Use: "install", Short: "校验并安装本地发行包，重启后验证", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if bundle == "" {
			return fmt.Errorf("请用 --bundle 指定已解压的发行目录。")
		}
		s, err := runtimeStore()
		if err != nil {
			return err
		}
		if err = s.Install(bundle, runtimeControl{}); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "安装完成，运行版本已验证。")
		return nil
	}}
	install.Flags().StringVar(&bundle, "bundle", "", "已解压的发行目录")
	root.AddCommand(install, &cobra.Command{Use: "rollback", Short: "恢复上一个已验证的版本", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		s, err := runtimeStore()
		if err != nil {
			return err
		}
		if err = s.Rollback(runtimeControl{}); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "已恢复上一个版本。")
		return nil
	}}, &cobra.Command{Use: "status", Short: "查看已安装版本与待恢复状态（JSON）", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		s, err := runtimeStore()
		if err != nil {
			return err
		}
		state, err := s.Read()
		if err != nil {
			return err
		}
		live, err := (runtimeControl{}).Running()
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{"installed": state, "running": live})
	}})
	return root
}
func lockRuntime() (func(), error) {
	dir, err := appdata.DataDir()
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return filelock.Acquire(filepath.Join(dir, "runtime.lock"))
}
