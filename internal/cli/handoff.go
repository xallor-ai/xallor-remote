package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/xallor-ai/xallor-remote/internal/appdata"
	"github.com/xallor-ai/xallor-remote/internal/handoff"
)

func cmdHandoff() *cobra.Command {
	return cmdHandoffWith(ensureRuntime, rpc)
}

func cmdHandoffWith(ensure func() error, call peerRPC) *cobra.Command {
	c := &cobra.Command{Use: "handoff", Short: "交接到指定 Cursor CLI 会话并查询回执"}
	var executable string
	var prefix []string
	configure := &cobra.Command{Use: "configure", Short: "仅在本机配置 Cursor CLI", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		dir, err := appdata.DataDir()
		if err != nil {
			return err
		}
		return handoff.Configure(dir, handoff.Config{Executable: executable, Prefix: prefix})
	}}
	configure.Flags().StringVar(&executable, "executable", "", "Cursor CLI 可执行文件绝对路径；JS 包使用 node.exe")
	configure.Flags().StringArrayVar(&prefix, "prefix-arg", nil, "固定启动前置参数；JS 包指定 index.js")
	_ = configure.MarkFlagRequired("executable")
	var device, file, submitSession string
	submit := &cobra.Command{Use: "submit", Short: "提交带固定请求 ID 的交接文件；重试不重复启动", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		f, err := os.Open(file)
		if err != nil {
			return err
		}
		defer f.Close()
		b, err := io.ReadAll(io.LimitReader(f, 64*1024+1))
		if err != nil {
			return err
		}
		if len(b) > 64*1024 {
			return fmt.Errorf("交接文件超过 64 KiB。")
		}
		var request handoff.Request
		if err = json.Unmarshal(b, &request); err != nil {
			return fmt.Errorf("交接文件不是有效 JSON。")
		}
		params := map[string]any{"device_id": device, "request": request}
		if submitSession != "" {
			params["session_id"] = submitSession
		}
		return printHandoff(cmd, "handoff.submit", params, ensure, call)
	}}
	submit.Flags().StringVar(&device, "device", "", "对方设备 ID")
	submit.Flags().StringVar(&file, "request", "", "完整交接 JSON 文件")
	submit.Flags().StringVar(&submitSession, "session-id", "", "已批准的访问会话 ID；不是交接文件中的 Cursor 会话 ID")
	_ = submit.MarkFlagRequired("device")
	_ = submit.MarkFlagRequired("request")
	var target, id, getSession string
	get := &cobra.Command{Use: "get", Short: "查询已有回执；不会启动或重放 Agent", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		params := map[string]any{"device_id": target, "request_id": id}
		if getSession != "" {
			params["session_id"] = getSession
		}
		return printHandoff(cmd, "handoff.get", params, ensure, call)
	}}
	get.Flags().StringVar(&target, "device", "", "对方设备 ID")
	get.Flags().StringVar(&id, "request-id", "", "原请求 ID")
	get.Flags().StringVar(&getSession, "session-id", "", "已批准的访问会话 ID")
	_ = get.MarkFlagRequired("device")
	_ = get.MarkFlagRequired("request-id")
	c.AddCommand(configure, submit, get)
	return c
}
func printHandoff(cmd *cobra.Command, method string, params map[string]any, ensure func() error, call peerRPC) error {
	if err := ensure(); err != nil {
		return err
	}
	result, err := call(method, params)
	if err != nil {
		return fmt.Errorf("交接结果未确认，请保留原请求 ID 并查询回执：%w", err)
	}
	if result["status"] != "completed" || result["exit_code"] != float64(0) || result["truncated"] == true {
		return fmt.Errorf("交接回执未完整返回；请用原请求 ID 查询。")
	}
	content, ok := result["content"].(string)
	if !ok {
		return fmt.Errorf("交接回执无效。")
	}
	var response struct {
		Receipt handoff.Receipt `json:"receipt"`
		Error   string          `json:"error"`
	}
	if json.Unmarshal([]byte(content), &response) != nil {
		return fmt.Errorf("交接回执无效。")
	}
	if response.Error != "" {
		return fmt.Errorf("%s", response.Error)
	}
	if response.Receipt.ID == "" {
		return fmt.Errorf("交接回执缺少请求 ID。")
	}
	return json.NewEncoder(cmd.OutOrStdout()).Encode(response.Receipt)
}
