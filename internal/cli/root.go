package cli

import (
	"encoding/json"
	"fmt"

	"log/slog"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/xallor-ai/xallor-remote/internal/identity"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/noderun"
)

func Execute() error {
	initConsole()
	root := &cobra.Command{
		Use:           "xallor-remote",
		Short:         "XallorRemote：经 Relay 远程执行",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	addProfileFormatGuard(root)
	root.AddCommand(cmdRelay(), cmdStart(), cmdEnsure(), cmdStop(), cmdStatus(), cmdDoctor(), cmdPolicy(), cmdEntitlement(), cmdGrant(), cmdInbound(), cmdRevoke(), cmdReset(), cmdPeer(), cmdExec(), cmdApprove(), cmdTUI(), cmdMCP(), cmdVersion(), cmdRuntime(), cmdIPCBridge(), cmdHandoff(), cmdAccount())
	root.AddCommand(cmdDevice())
	root.AddCommand(cmdConfig())
	return executeCLI(root, os.Args[1:])
}

func cmdStart() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "前台运行 Runtime",
		RunE: func(cmd *cobra.Command, args []string) error {
			unlock, err := lockRuntime()
			if err != nil {
				return err
			}
			defer unlock()
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

func cmdGrant() *cobra.Command {
	c := &cobra.Command{Use: "grant", Short: "本机授权码"}
	c.AddCommand(&cobra.Command{
		Use:   "issue",
		Short: "签发新授权码并打开入站（仅本机）",
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
	return cmdPeerWith(ensureRuntime, rpc)
}

type peerRPC func(string, map[string]any) (map[string]any, error)

func cmdPeerWith(ensure func() error, call peerRPC) *cobra.Command {
	c := &cobra.Command{Use: "peer", Short: "对方设备"}
	var device, grant string
	add := &cobra.Command{
		Use:   "add",
		Short: "添加可控制的设备",
		RunE: func(cmd *cobra.Command, args []string) error {
			if device == "" {
				return fmt.Errorf("需要 --device")
			}
			if err := ensure(); err != nil {
				return err
			}
			_, err := call("peer.add", map[string]any{"device_id": device, "grant": grant})
			if err != nil {
				return err
			}
			fmt.Println("已添加", device)
			return nil
		},
	}
	add.Flags().StringVar(&device, "device", "", "对方设备 ID")
	add.Flags().StringVar(&grant, "grant", "", "未认领设备的共享授权码；账号授权访问可省略")
	c.AddCommand(add)
	c.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "列出 peer",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := ensure(); err != nil {
				return err
			}
			res, err := call("peer.list", map[string]any{})
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
	var removeDevice string
	rm := &cobra.Command{
		Use:   "remove",
		Short: "删除对方设备",
		RunE: func(cmd *cobra.Command, args []string) error {
			if removeDevice == "" {
				return fmt.Errorf("需要 --device")
			}
			if err := ensure(); err != nil {
				return err
			}
			if _, err := call("peer.remove", map[string]any{"device_id": removeDevice}); err != nil {
				return err
			}
			fmt.Println("已删除", removeDevice)
			return nil
		},
	}
	rm.Flags().StringVar(&removeDevice, "device", "", "对方设备 ID")
	c.AddCommand(rm)
	c.AddCommand(cmdPeerRenameWith(ensure, call))
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

func rpc(method string, params map[string]any) (map[string]any, error) {
	raw, err := ipc.Dial(2 * time.Second)
	if err != nil {
		return nil, fmt.Errorf("Runtime 未运行，请先 xallor-remote start 或 ensure")
	}
	defer raw.Close()
	_ = raw.SetDeadline(time.Now().Add(10 * time.Second))
	c := ipc.NewConn(raw)
	if err := ipc.Call(c, "1", method, params); err != nil {
		return nil, err
	}
	fr, err := c.ReadFrame()
	if err != nil {
		return nil, err
	}
	if fr.OK == nil || !*fr.OK {
		return nil, fmt.Errorf("%s", ipc.FailureMessage(fr))
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
