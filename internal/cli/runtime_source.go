package cli

import (
	"context"
	"encoding/json"
	"time"

	"github.com/spf13/cobra"
	"github.com/xallor-ai/xallor-remote/internal/release"
)

func cmdRuntimeVerify() *cobra.Command {
	var bundle, proof string
	command := &cobra.Command{Use: "verify", Short: "验证本地发行包来源并输出 JSON，不安装或执行", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(command.Context(), 60*time.Second)
			defer cancel()
			result, err := release.VerifyBundleSource(ctx, bundle, proof)
			if err != nil {
				return err
			}
			return json.NewEncoder(command.OutOrStdout()).Encode(result)
		}}
	command.Flags().StringVar(&bundle, "bundle", "", "已解压的发行目录")
	command.Flags().StringVar(&proof, "attestation-bundle", "", "Sigstore 证明文件；不改变内置信任政策")
	_ = command.MarkFlagRequired("bundle")
	return command
}
