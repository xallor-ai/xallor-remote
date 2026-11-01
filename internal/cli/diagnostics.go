package cli

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/xallor-ai/xallor-remote/internal/diagnostics"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

func cmdStatus() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{Use: "status", Short: "本机状态", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !asJSON {
				return printStatus()
			}
			res, err := rpc("status", map[string]any{})
			if err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(res)
		}}
	c.Flags().BoolVar(&asJSON, "json", false, "输出 JSON")
	return c
}

func cmdDoctor() *cobra.Command {
	var device string
	var asJSON bool
	c := &cobra.Command{Use: "doctor", Short: "检查连接并给出下一步建议", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if device != "" && !protocol.IsNumericDeviceID(device) {
				return fmt.Errorf("需要有效的 9 位设备 ID。")
			}
			r := observe(device)
			if asJSON {
				if err := json.NewEncoder(cmd.OutOrStdout()).Encode(r); err != nil {
					return err
				}
			} else {
				labels := map[string]string{"ipc": "本机服务", "runtime_relay": "本机连接", "peer": "配对信息", "relay_transport": "中转连接", "authorization": "授权", "target_online": "目标在线", "inbound": "允许访问", "target_runtime": "目标响应"}
				states := map[string]string{"ok": "正常", "fail": "失败", "unknown": "未确认", "skipped": "未检查"}
				for _, check := range r.Checks {
					fmt.Fprintf(cmd.OutOrStdout(), "%s：%s %s\n", labels[check.Layer], states[check.State], check.Action)
				}
			}
			if !r.OK {
				return fmt.Errorf("连接检查未全部通过。")
			}
			return nil
		}}
	c.Flags().StringVar(&device, "device", "", "对方的 9 位设备 ID；省略时只检查本机")
	c.Flags().BoolVar(&asJSON, "json", false, "输出 JSON")
	return c
}

// Do not call ensure or identity.Load here: observation must leave a stopped or absent profile untouched.
func observe(device string) diagnostics.Report {
	r := diagnostics.New(device)
	raw, err := ipc.Dial(time.Second)
	if err != nil {
		r.Set("ipc", "fail", "unavailable", "本机服务不可访问，请检查服务是否已启动。")
		return r
	}
	defer raw.Close()
	_ = raw.SetDeadline(time.Now().Add(7 * time.Second))
	c := ipc.NewConn(raw)
	r.Set("ipc", "unknown", "invalid_response", "本机服务未完成应答，请检查版本或重新诊断。")
	if ipc.Call(c, "doctor", "doctor", map[string]any{"device_id": device}) != nil {
		return r
	}
	f, err := c.ReadFrame()
	if err != nil || f.ID != "doctor" || f.OK == nil || !*f.OK {
		return r
	}
	var remote diagnostics.Report
	if json.Unmarshal(f.Result, &remote) != nil || remote.Schema != 1 || len(remote.Checks) != len(r.Checks) {
		return r
	}
	return remote
}
