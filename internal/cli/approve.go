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
			raw, err := ipc.Dial(2 * time.Second)
			if err != nil {
				return fmt.Errorf("Runtime 未运行，请先 xallor-remote start 或 ensure")
			}
			defer raw.Close()
			c := ipc.NewConn(raw)
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
					ExecID  string `json:"exec_id"`
					Preview string `json:"preview"`
				}
				_ = json.Unmarshal(ev.Params, &p)
				fmt.Printf("\n需要确认的命令:\n  %s\n允许执行？[y/N] ", p.Preview)
				line, _ := in.ReadString('\n')
				allow := strings.EqualFold(strings.TrimSpace(line), "y")
				if err := ipc.Call(c, ev.ID, "approval.respond", map[string]any{"allow": allow}); err != nil {
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
