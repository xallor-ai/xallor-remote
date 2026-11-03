package cli

import (
	"log/slog"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/admission"
	"github.com/xallor-ai/xallor-remote/internal/appdata"
	"github.com/xallor-ai/xallor-remote/internal/principal"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
	"github.com/xallor-ai/xallor-remote/internal/relay"
	"github.com/xallor-ai/xallor-remote/internal/storage"
)

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
			store, err := storage.OpenSQLStore(data)
			if err != nil {
				return err
			}
			defer store.Close()
			hub := relay.NewHub(store, principal.NewService(store), access.NewService(store), admission.New(admission.Limits{PerDevice: protocol.MaxDeviceRequests}))
			return relay.Serve(listen, hub, log, relay.Quota{Enabled: quota})
		},
	}
	c.Flags().StringVar(&listen, "listen", ":8443", "监听地址")
	c.PersistentFlags().StringVar(&data, "data", "", "数据目录")
	c.AddCommand(cmdRelayPrincipal(&data))
	c.Flags().BoolVar(&quota, "quota", false, "启用源 IP 限额")
	return c
}
