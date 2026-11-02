package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/xallor-ai/xallor-remote/internal/execution"
)

func cmdExec() *cobra.Command {
	return executionCommand(func(device string, request execution.Request) error {
		if err := ensureRuntime(); err != nil {
			return err
		}
		return streamExec(device, request)
	})
}

func executionCommand(run func(string, execution.Request) error) *cobra.Command {
	var device, shell, scriptFile, program, requestFile, cwd string
	var timeout int
	c := &cobra.Command{
		Use:   "exec [--script-file FILE | --program PROGRAM | --request-file FILE] -- [script or arguments]",
		Short: "在对方执行脚本或程序",
		RunE: func(cmd *cobra.Command, args []string) error {
			modes := 0
			for _, name := range []string{"script-file", "program", "request-file"} {
				if cmd.Flags().Changed(name) {
					modes++
				}
			}
			if modes > 1 {
				return fmt.Errorf("请选择一种输入：脚本文件、程序或请求文件。")
			}
			r := execution.Request{Cwd: cwd, TimeoutMS: timeout}
			switch {
			case cmd.Flags().Changed("request-file"):
				if len(args) != 0 || cmd.Flags().Changed("shell") || cmd.Flags().Changed("cwd") || cmd.Flags().Changed("timeout-ms") {
					return fmt.Errorf("使用请求文件时，请把执行参数写入文件。")
				}
				b, err := readExecutionInput(requestFile, execution.MaxJSONBytes, cmd.InOrStdin())
				if err != nil {
					return err
				}
				r, err = execution.Decode(b)
				if err != nil {
					return fmt.Errorf("请求文件无效：%w", err)
				}
			case cmd.Flags().Changed("program"):
				if cmd.Flags().Changed("shell") {
					return fmt.Errorf("程序模式不使用 --shell。")
				}
				r.Program = &execution.Program{Executable: program, Args: append([]string{}, args...)}
			case cmd.Flags().Changed("script-file"):
				if len(args) != 0 {
					return fmt.Errorf("脚本文件模式不接受额外参数。")
				}
				b, err := readExecutionInput(scriptFile, execution.MaxInputBytes, cmd.InOrStdin())
				if err != nil {
					return err
				}
				r.Script = &execution.Script{Text: string(b), Shell: shell}
			default:
				if len(args) != 1 {
					return fmt.Errorf("请把脚本作为一个完整参数传入，或使用 --script-file；程序参数请使用 --program。")
				}
				r.Script = &execution.Script{Text: args[0], Shell: shell}
			}
			if err := r.Validate(); err != nil {
				return fmt.Errorf("执行参数无效：%w", err)
			}
			return run(device, r)
		},
	}
	c.Flags().StringVar(&device, "device", "", "对方设备 ID（仅一台时可省）")
	c.Flags().StringVar(&shell, "shell", "", "对方解释器：auto/powershell/pwsh/bash/sh（省略时使用对方配置）")
	c.Flags().StringVar(&scriptFile, "script-file", "", "读取本地 UTF-8 脚本；- 表示标准输入")
	c.Flags().StringVar(&requestFile, "request-file", "", "读取本地执行请求 JSON；- 表示标准输入")
	c.Flags().StringVar(&program, "program", "", "对方程序路径或名称，-- 后逐个传递程序参数")
	c.Flags().StringVar(&cwd, "cwd", "", "对方工作目录（默认 workspace）")
	c.Flags().IntVar(&timeout, "timeout-ms", 0, "执行超时毫秒，0 不设期限，最长 3600000")
	return c
}

func readExecutionInput(name string, limit int, stdin io.Reader) ([]byte, error) {
	var reader io.Reader = stdin
	if name != "-" {
		f, err := os.Open(name)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("请输入普通文件路径。")
		}
		reader = f
	}
	b, err := io.ReadAll(io.LimitReader(reader, int64(limit)+4))
	if err != nil {
		return nil, err
	}
	b = bytes.TrimPrefix(b, []byte{0xef, 0xbb, 0xbf})
	if len(b) > limit {
		return nil, execution.ErrTooLarge
	}
	return b, nil
}
