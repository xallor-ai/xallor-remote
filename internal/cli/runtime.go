package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/spf13/cobra"
	"github.com/xallor-ai/xallor-remote/internal/appdata"
	"github.com/xallor-ai/xallor-remote/internal/filelock"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/release"
	"github.com/xallor-ai/xallor-remote/internal/runtimeowner"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

func runtimeStore() (release.Store, error) {
	dir, err := appdata.DataDir()
	return release.Store{Dir: filepath.Join(dir, "runtime")}, err
}
func cmdRuntime() *cobra.Command {
	root := &cobra.Command{Use: "runtime", Short: "验证来源、安装、查看或回滚本机版本"}
	root.AddCommand(cmdRuntimeVerify())
	root.AddCommand(cmdRuntimeReconcile())
	var bundle string
	var operationID string
	install := &cobra.Command{Use: "install", Short: "校验并安装本地发行包，重启后验证", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if bundle == "" {
			return fmt.Errorf("请用 --bundle 指定已解压的发行目录。")
		}
		if handled, err := ownerLifecycle(cmd.Context(), "install", bundle, cmd.ErrOrStderr()); handled {
			if err == nil {
				fmt.Fprintln(cmd.OutOrStdout(), "安装事务已完成，版本确认通过。")
			}
			return err
		}
		if err := coldLifecycle(cmd.Context(), "install", bundle, cmd.ErrOrStderr()); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "安装完成，运行版本已验证。")
		return nil
	}}
	install.Flags().StringVar(&bundle, "bundle", "", "已解压的发行目录")
	root.AddCommand(install, &cobra.Command{Use: "rollback", Short: "恢复上一个已验证的版本", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if handled, err := ownerLifecycle(cmd.Context(), "rollback", "", cmd.ErrOrStderr()); handled {
			if err == nil {
				fmt.Fprintln(cmd.OutOrStdout(), "回滚事务已完成，版本确认通过。")
			}
			return err
		}
		if err := coldLifecycle(cmd.Context(), "rollback", "", cmd.ErrOrStderr()); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "已恢复上一个版本。")
		return nil
	}}, &cobra.Command{Use: "status", Short: "查看已安装版本与待恢复状态（JSON）", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		s, err := runtimeStore()
		if err != nil {
			return err
		}
		if operationID != "" {
			record, settled, err := (runtimeowner.Journal{Dir: filepath.Join(s.Dir, "lifecycle")}).QuerySettled(operationID)
			if err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{"operation": record, "settled": settled})
		}
		state, err := s.Read()
		if err != nil {
			return err
		}
		live, err := (runtimeControl{}).Running()
		if err != nil {
			return err
		}
		result := map[string]any{"installed": state, "running": live}
		operation, observeErr := (runtimeowner.Journal{Dir: filepath.Join(s.Dir, "lifecycle")}).Active()
		if observeErr == nil {
			result["operation"] = operation
		} else if !errors.Is(observeErr, runtimeowner.ErrUnknown) {
			return observeErr
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
	}})
	for _, command := range root.Commands() {
		if command.Name() == "status" {
			command.Flags().StringVar(&operationID, "operation", "", "只读查询指定版本事务")
		}
	}
	return root
}

func cmdRuntimeReconcile() *cobra.Command {
	return cmdRuntimeReconcileWith(ipc.Dial)
}

func cmdRuntimeReconcileWith(dial func(time.Duration) (net.Conn, error)) *cobra.Command {
	var operationID, digest string
	var revision int64
	cmd := &cobra.Command{Use: "reconcile", Short: "只读观察未确认的本机事务", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if !regexp.MustCompile(`^[a-f0-9]{32}$`).MatchString(operationID) || revision < 1 || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(digest) {
			return fmt.Errorf("需要有效的 --operation、正整数 --revision 和 64 位十六进制 --digest。")
		}
		dir, err := appdata.DataDir()
		if err != nil {
			return err
		}
		ns, err := (runtimeowner.Journal{Dir: filepath.Join(dir, "runtime", "lifecycle")}).NamespaceID()
		if err != nil {
			return err
		}
		raw, err := dial(2 * time.Second)
		if err != nil {
			return fmt.Errorf("Runtime 未运行，请先 xallor-remote start 或 ensure")
		}
		defer raw.Close()
		_ = raw.SetDeadline(time.Now().Add(10 * time.Second))
		c := ipc.NewConn(raw)
		params := map[string]any{"operation_id": operationID, "namespace_sha256": ns, "expected_revision": revision, "expected_record_sha256": digest}
		if err := ipc.Call(c, "reconcile", "runtime.operation.reconcile", params); err != nil {
			return err
		}
		frame, err := c.ReadFrame()
		if err != nil {
			return err
		}
		if frame.ID != "reconcile" || frame.Event != "" || frame.OK == nil {
			return fmt.Errorf("事务观察响应无效。")
		}
		if !*frame.OK {
			return fmt.Errorf("%s", ipc.FailureMessage(frame))
		}
		if frame.Code != "" {
			return fmt.Errorf("事务观察响应无效。")
		}
		var out json.RawMessage
		if err := json.Unmarshal(frame.Result, &out); err != nil {
			return err
		}
		var pretty any
		if err := json.Unmarshal(out, &pretty); err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(pretty)
	}}
	cmd.Flags().StringVar(&operationID, "operation", "", "本机升级事务 ID")
	cmd.Flags().Int64Var(&revision, "revision", 0, "读取时的事务 revision")
	cmd.Flags().StringVar(&digest, "digest", "", "读取时的 record SHA-256")
	return cmd
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
