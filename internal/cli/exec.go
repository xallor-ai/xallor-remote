package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/xallor-ai/xallor-remote/internal/execution"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

func cmdExec() *cobra.Command {
	return executionCommand(func(cmd *cobra.Command, device string, request execution.Request) error {
		jsonl, _ := cmd.Flags().GetBool("jsonl")
		if err := ensureRuntime(); err != nil {
			return localExecError(cmd.OutOrStdout(), jsonl, protocol.RuntimeUnavailable, err)
		}
		return streamExec(device, request, jsonl, cmd.OutOrStdout(), cmd.ErrOrStderr())
	})
}

func executionCommand(run func(*cobra.Command, string, execution.Request) error) *cobra.Command {
	var device, shell, scriptFile, program, requestFile, cwd string
	var timeout int
	c := &cobra.Command{
		Use:   "exec [--script-file FILE | --program PROGRAM | --request-file FILE] -- [script or arguments]",
		Short: "在对方执行脚本或程序",
		Long:  "在对方执行脚本或程序。使用 --jsonl 时，参数解析错误也在 stdout 返回一条 invalid_execution JSON 事件，退出码非零；stderr 保留诊断。--help 仍显示普通帮助。\n\nExecute a script or program on the target. With --jsonl, argument parsing errors emit one invalid_execution JSON event on stdout and exit nonzero; stderr retains diagnostics. --help remains plain help.",
		RunE: func(cmd *cobra.Command, args []string) (runErr error) {
			dispatched := false
			defer func() {
				if runErr != nil && !dispatched {
					jsonl, _ := cmd.Flags().GetBool("jsonl")
					code := protocol.InvalidExecution
					if errors.Is(runErr, execution.ErrTooLarge) {
						code = protocol.TooLarge
					}
					runErr = localExecError(cmd.OutOrStdout(), jsonl, code, runErr)
				}
			}()
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
			dispatched = true
			return run(cmd, device, r)
		},
	}
	c.Flags().StringVar(&device, "device", "", "对方设备 ID（仅一台时可省）")
	c.Flags().Bool("jsonl", false, "逐行 JSON 事件（含参数错误） / JSONL events, including argument errors")
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
