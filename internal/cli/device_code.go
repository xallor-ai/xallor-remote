package cli

import (
	"errors"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/xallor-ai/xallor-remote/internal/access"
)

var errCodeParameters = errors.New("授权码参数无效；新 secret 请使用 XALLOR_REMOTE_CODE_SECRET 或 --secret-stdin。")

// Validate all local inputs before the runner can ensure/contact Runtime.
func cmdDeviceCode(run func(*cobra.Command, map[string]any) error) *cobra.Command {
	root := &cobra.Command{Use: "code", Short: "管理逻辑授权码（仅当前设备 owner）"}
	var codeID string
	revoke := &cobra.Command{Use: "revoke", Short: "撤销逻辑授权码及其派生 session", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if (access.Request{Action: "code.revoke", CodeID: codeID}).Validate() != nil {
			return errCodeParameters
		}
		return run(cmd, map[string]any{"action": "code.revoke", "code_id": codeID})
	}}
	revoke.Flags().StringVar(&codeID, "code-id", "", "逻辑授权码 ID")
	_ = revoke.MarkFlagRequired("code-id")
	var oldID, newID string
	var fromStdin bool
	rotate := &cobra.Command{Use: "rotate", Short: "轮换逻辑授权码并返回替代码状态，不显示 secret", Args: func(_ *cobra.Command, args []string) error {
		if len(args) != 0 {
			return errCodeParameters
		}
		return nil
	}, RunE: func(cmd *cobra.Command, _ []string) error {
		secret, err := readCodeRotationSecret(cmd.InOrStdin(), fromStdin)
		if err != nil {
			return err
		}
		req := access.Request{Action: "code.rotate", CodeID: oldID, NewCodeID: newID, NewSecret: secret}
		if req.Validate() != nil || oldID == newID {
			return errCodeParameters
		}
		return run(cmd, map[string]any{"action": req.Action, "code_id": req.CodeID, "new_code_id": req.NewCodeID, "new_secret": req.NewSecret})
	}}
	// Parser errors can include an invalid flag value; never echo it here.
	rotate.SetFlagErrorFunc(func(*cobra.Command, error) error { return errCodeParameters })
	rotate.Flags().StringVar(&oldID, "code-id", "", "旧逻辑授权码 ID")
	rotate.Flags().StringVar(&newID, "new-code-id", "", "新逻辑授权码 ID")
	rotate.Flags().BoolVar(&fromStdin, "secret-stdin", false, "从标准输入读取新 secret（最多 4096 UTF-8 字节，可带一个终止换行）")
	_ = rotate.MarkFlagRequired("code-id")
	_ = rotate.MarkFlagRequired("new-code-id")
	root.AddCommand(revoke, rotate)
	return root
}

func readCodeRotationSecret(input io.Reader, fromStdin bool) (string, error) {
	secret, configured := os.LookupEnv("XALLOR_REMOTE_CODE_SECRET")
	if fromStdin && configured {
		return "", errors.New("环境变量与 --secret-stdin 不能同时提供 secret。")
	}
	if !fromStdin {
		if !configured {
			return "", errors.New("请通过 XALLOR_REMOTE_CODE_SECRET 或 --secret-stdin 提供新 secret。")
		}
		return secret, nil // Empty values remain invalid; the shared request validator checks them.
	}
	raw, err := io.ReadAll(io.LimitReader(input, int64(access.MaxCodeSecretBytes+2+1)))
	if err != nil {
		return "", errors.New("读取新 secret 失败。")
	}
	secret = string(raw)
	if strings.HasSuffix(secret, "\n") {
		secret = strings.TrimSuffix(secret, "\n")
		secret = strings.TrimSuffix(secret, "\r")
	}
	return secret, nil // Preserve all remaining bytes; validation never trims whitespace.
}
