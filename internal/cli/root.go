package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/xallor-ai/xallor-remote/internal/appdata"
	"github.com/xallor-ai/xallor-remote/internal/identity"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/noderun"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
	"github.com/xallor-ai/xallor-remote/internal/relay"
)

func Execute() error {
	initConsole()
	root := &cobra.Command{
		Use:           "xallor-remote",
		Short:         "XallorRemote：经 Relay 远程执行",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(cmdRelay(), cmdStart(), cmdEnsure(), cmdStop(), cmdStatus(), cmdGrant(), cmdInbound(), cmdRevoke(), cmdReset(), cmdPeer(), cmdExec(), cmdApprove(), cmdTUI(), cmdMCP())
	return root.Execute()
}

func cmdRelay() *cobra.Command {
	var listen, data string
	var quota bool
	c := &cobra.Command{
		Use:   "relay",
		Short: "运行中转",
		RunE: func(cmd *cobra.Command, args []string) error {
			if data == "" {
				dir, err := appdata.DataDir()
				if err != nil {
					return err
				}
				data = filepath.Join(dir, "relay")
			}
			log := slog.New(slog.NewTextHandler(os.Stderr, nil))
			return relay.Serve(listen, data, log, relay.Quota{Enabled: quota})
		},
	}
	c.Flags().StringVar(&listen, "listen", ":8443", "监听地址")
	c.Flags().StringVar(&data, "data", "", "数据目录")
	c.Flags().BoolVar(&quota, "quota", false, "启用源 IP 限额")
	return c
}

func cmdStart() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "前台运行 Runtime",
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := identity.Load()
			if err != nil {
				return err
			}
			printBanner(st)
			log := slog.New(slog.NewTextHandler(os.Stderr, nil))
			return noderun.New(st, log).Run()
		},
	}
}

func cmdEnsure() *cobra.Command {
	return &cobra.Command{
		Use:   "ensure",
		Short: "确保 Runtime 在跑",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := ensureRuntime(); err != nil {
				return err
			}
			return printStatus()
		},
	}
}

func cmdStatus() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "本机状态",
		RunE: func(cmd *cobra.Command, args []string) error {
			return printStatus()
		},
	}
}

func cmdGrant() *cobra.Command {
	c := &cobra.Command{Use: "grant", Short: "本机授权码"}
	c.AddCommand(&cobra.Command{
		Use:   "issue",
		Short: "签发（仅本机）",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := ensureRuntime(); err != nil {
				return err
			}
			res, err := rpc("grant.issue", map[string]any{})
			if err != nil {
				return err
			}
			fmt.Printf("授权码: %s    ← 把这一行给对方\n", res["grant"])
			return nil
		},
	})
	c.AddCommand(&cobra.Command{
		Use:   "rotate",
		Short: "换新授权码，旧码立刻失效",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := ensureRuntime(); err != nil {
				return err
			}
			res, err := rpc("grant.rotate", map[string]any{})
			if err != nil {
				return err
			}
			fmt.Printf("授权码: %s    ← 把这一行给对方\n", res["grant"])
			return nil
		},
	})
	c.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "再显示",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := ensureRuntime(); err != nil {
				return err
			}
			res, err := rpc("grant.show", map[string]any{})
			if err != nil {
				return err
			}
			g, _ := res["grant"].(string)
			if g == "" {
				fmt.Println("还没有授权码。需要时在本机执行 xallor-remote grant issue")
				return nil
			}
			fmt.Printf("授权码: %s\n", g)
			return nil
		},
	})
	return c
}

func cmdPeer() *cobra.Command {
	c := &cobra.Command{Use: "peer", Short: "对方设备"}
	var id, grant string
	add := &cobra.Command{
		Use:   "add",
		Short: "添加可控制的设备",
		RunE: func(cmd *cobra.Command, args []string) error {
			if id == "" || grant == "" {
				return fmt.Errorf("需要 --id 与 --grant")
			}
			if err := ensureRuntime(); err != nil {
				return err
			}
			_, err := rpc("peer.add", map[string]any{"device_id": id, "grant": grant})
			if err != nil {
				return err
			}
			fmt.Println("已添加", id)
			return nil
		},
	}
	add.Flags().StringVar(&id, "id", "", "对方设备 ID")
	add.Flags().StringVar(&grant, "grant", "", "对方授权码")
	c.AddCommand(add)
	c.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "列出 peer",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := ensureRuntime(); err != nil {
				return err
			}
			res, err := rpc("peer.list", map[string]any{})
			if err != nil {
				return err
			}
			raw, _ := res["peers"].([]any)
			if len(raw) == 0 {
				fmt.Println("还没有对方设备。")
				return nil
			}
			for _, p := range raw {
				fmt.Println(p)
			}
			return nil
		},
	})
	var rid string
	rm := &cobra.Command{
		Use:   "remove",
		Short: "删除对方设备",
		RunE: func(cmd *cobra.Command, args []string) error {
			if rid == "" {
				return fmt.Errorf("需要 --id")
			}
			if err := ensureRuntime(); err != nil {
				return err
			}
			if _, err := rpc("peer.remove", map[string]any{"device_id": rid}); err != nil {
				return err
			}
			fmt.Println("已删除", rid)
			return nil
		},
	}
	rm.Flags().StringVar(&rid, "id", "", "对方设备 ID")
	c.AddCommand(rm)
	return c
}

func cmdExec() *cobra.Command {
	var device string
	c := &cobra.Command{
		Use:   "exec -- [command]",
		Short: "在对方设备执行",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := ensureRuntime(); err != nil {
				return err
			}
			command := args[0]
			if len(args) > 1 {
				for _, a := range args[1:] {
					command += " " + a
				}
			}
			return streamExec(device, command)
		},
	}
	c.Flags().StringVar(&device, "device", "", "目标 device_id（仅一台时可省）")
	return c
}

func printBanner(st *identity.Store) {
	id, _, grant, ws, relay, inbound, _ := st.Snapshot()
	fmt.Printf("Relay:     %s\n", relay)
	fmt.Printf("Device ID: %s\n", id)
	fmt.Printf("Workspace: %s\n", ws)
	if inbound && grant != "" {
		fmt.Println("入站:      开")
	} else if grant != "" {
		fmt.Println("入站:      关")
	} else {
		fmt.Println("入站:      关（还没有授权码）")
	}
}

func printStatus() error {
	res, err := rpc("status", map[string]any{})
	if err != nil {
		return err
	}
	fmt.Printf("Device ID: %v\n", res["device_id"])
	fmt.Printf("Workspace: %v\n", res["workspace"])
	fmt.Printf("Relay:     %v\n", res["relay"])
	fmt.Printf("入站:      %v\n", res["inbound"])
	fmt.Printf("在线:      %v\n", res["online"])
	fmt.Printf("版本:      %v\n", res["version"])
	return nil
}

func ensureRuntime() error {
	if _, err := ipc.Dial(500 * time.Millisecond); err == nil {
		return nil
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(self, "start")
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := detach(cmd); err != nil {
		return err
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if c, err := ipc.Dial(200 * time.Millisecond); err == nil {
			_ = c.Close()
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("无法拉起 Runtime。找不到二进制或 IPC 未就绪。")
}

func rpc(method string, params map[string]any) (map[string]any, error) {
	raw, err := ipc.Dial(2 * time.Second)
	if err != nil {
		return nil, fmt.Errorf("Runtime 未运行，请先 xallor-remote start 或 ensure")
	}
	defer raw.Close()
	c := ipc.NewConn(raw)
	if err := ipc.Call(c, "1", method, params); err != nil {
		return nil, err
	}
	fr, err := c.ReadFrame()
	if err != nil {
		return nil, err
	}
	if fr.OK == nil || !*fr.OK {
		return nil, fmt.Errorf("%s", ipc.Human(fr.Code))
	}
	var out map[string]any
	if len(fr.Result) > 0 {
		_ = json.Unmarshal(fr.Result, &out)
	}
	if out == nil {
		out = map[string]any{}
	}
	return out, nil
}

func streamExec(device, command string) error {
	raw, err := ipc.Dial(2 * time.Second)
	if err != nil {
		return err
	}
	defer raw.Close()
	c := ipc.NewConn(raw)
	if err := ipc.Call(c, "e1", "exec", map[string]any{"device_id": device, "command": command}); err != nil {
		return err
	}
	for {
		fr, err := c.ReadFrame()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if fr.Event == "stdout" {
			fmt.Fprint(os.Stdout, fr.Data)
			continue
		}
		if fr.Event == "stderr" {
			fmt.Fprint(os.Stderr, fr.Data)
			continue
		}
		if fr.OK != nil && *fr.OK {
			var res struct {
				Status string `json:"status"`
			}
			if len(fr.Result) > 0 {
				_ = json.Unmarshal(fr.Result, &res)
			}
			switch res.Status {
			case protocol.ExitCancelled:
				return fmt.Errorf("%s", ipc.Human(protocol.Cancelled))
			case protocol.ExitTimeout:
				return fmt.Errorf("%s", ipc.Human(protocol.ExecTimeout))
			}
			return nil
		}
		if fr.OK != nil && !*fr.OK {
			return fmt.Errorf("%s", ipc.Human(fr.Code))
		}
	}
}

