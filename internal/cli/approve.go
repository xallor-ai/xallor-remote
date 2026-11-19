package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/runtimehead"
)

func cmdApprove() *cobra.Command {
	return &cobra.Command{
		Use:   "approve",
		Short: "等待并确认对本机的高危命令",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !stdinIsTTY() {
				return fmt.Errorf("当前没有交互终端。无界面时高危命令会直接拒绝。")
			}
			if err := ensureRuntime(); err != nil {
				return err
			}
			c, err := runtimehead.Dial(2 * time.Second)
			if err != nil {
				return err
			}
			defer c.Close()
			if err := ipc.Call(c, "sub1", "approval.subscribe", map[string]any{}); err != nil {
				return err
			}
			fr, err := c.ReadFrame()
			if err != nil {
				return err
			}
			if fr.OK == nil || !*fr.OK {
				return fmt.Errorf("%s", ipc.Human(fr.Code))
			}
			fmt.Println("已开始监听审批。对方发来需确认的命令时会提示你。Ctrl+C 退出。")
			in := bufio.NewReader(os.Stdin)
			for {
				ev, err := c.ReadFrame()
				if err != nil {
					return err
				}
				if ev.Event != "approval" {
					continue
				}
				var p struct {
					ExecID        string          `json:"exec_id"`
					RequestSHA256 string          `json:"request_sha256"`
					Details       json.RawMessage `json:"details"`
				}
				_ = json.Unmarshal(ev.Params, &p)
				if p.RequestSHA256 == "" || len(p.Details) == 0 {
					return fmt.Errorf("审批请求缺少不可变操作详情。")
				}
				fmt.Printf("\n需要确认的完整操作（摘要 %s）:\n%s\n允许执行？[y/N] ", p.RequestSHA256, string(p.Details))
				line, _ := in.ReadString('\n')
				allow := strings.EqualFold(strings.TrimSpace(line), "y")
				if err := ipc.Call(c, ev.ID, "approval.respond", map[string]any{"approval_id": ev.ID, "request_sha256": p.RequestSHA256, "allow": allow}); err != nil {
					return err
				}
				ack, err := c.ReadFrame()
				if err != nil {
					return err
				}
				if ack.OK != nil && !*ack.OK {
					fmt.Println(ipc.Human(ack.Code))
					continue
				}
				if allow {
					fmt.Println("已允许。")
				} else {
					fmt.Println("已拒绝。")
				}
			}
		},
	}
}
