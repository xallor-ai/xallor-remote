package cli

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/admission"
	"github.com/xallor-ai/xallor-remote/internal/appdata"
	"github.com/xallor-ai/xallor-remote/internal/delegation"
	"github.com/xallor-ai/xallor-remote/internal/principal"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
	"github.com/xallor-ai/xallor-remote/internal/relay"
	"github.com/xallor-ai/xallor-remote/internal/storage"
)

func cmdRelay() *cobra.Command {
	var listen, data string
	var quota bool
	var transport relay.Transport
	c := &cobra.Command{
		Use:   "relay",
		Short: "运行中转",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := transport.Validate(listen); err != nil {
				return err
			}
			if data == "" {
				dir, err := appdata.DataDir()
				if err != nil {
					return err
				}
				data = filepath.Join(dir, "relay")
			}
			log := slog.New(slog.NewTextHandler(os.Stderr, nil))
			store, err := storage.OpenSQLStore(data)
			if err != nil {
				return err
			}
			defer store.Close()
			key, err := store.ExecutionKey()
			if err != nil {
				return err
			}
			accounts, permissions := principal.NewService(store), access.NewService(store)
			authority := delegation.NewAuthority(key, store, accounts, permissions)
			hub := relay.NewHub(store, accounts, permissions, authority, admission.New(admission.Limits{PerDevice: protocol.MaxDeviceRequests}))
			return relay.Serve(listen, hub, log, relay.Quota{Enabled: quota}, transport)
		},
	}
	c.Flags().StringVar(&listen, "listen", "127.0.0.1:8443", "监听地址；非本机地址需要 TLS 或安全反向代理")
	c.Flags().StringVar(&transport.CertFile, "tls-cert", "", "TLS 证书 PEM 文件")
	c.Flags().StringVar(&transport.KeyFile, "tls-key", "", "TLS 私钥 PEM 文件")
	c.Flags().BoolVar(&transport.ReverseProxy, "reverse-proxy", false, "仅接受可信 HTTPS 反向代理的请求")
	c.Flags().StringArrayVar(&transport.TrustedProxies, "trusted-proxy", nil, "可信代理 IP 或 CIDR（可重复）")
	c.Flags().StringArrayVar(&transport.AllowedOrigins, "allowed-origin", nil, "允许的浏览器 Origin（完整 http(s) 地址，可重复）")
	c.PersistentFlags().StringVar(&data, "data", "", "数据目录")
	c.AddCommand(cmdRelayPrincipal(&data))
	c.AddCommand(&cobra.Command{Use: "registry", Short: "显示单 Relay sticky registry 契约", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(protocol.RelayRegistry())
	}})
	c.AddCommand(&cobra.Command{Use: "failover", Short: "显示多地域 failover 契约（当前仅说明）", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(protocol.RelayFailover())
	}})
	c.AddCommand(&cobra.Command{Use: "planes", Short: "显示 control/data plane 分离契约", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(protocol.ControlDataPlane())
	}})
	c.AddCommand(&cobra.Command{Use: "oidc", Short: "显示 OIDC 外部主体映射契约", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(protocol.OIDCMapping())
	}})
	c.Flags().BoolVar(&quota, "quota", false, "启用源 IP 限额")
	return c
}
