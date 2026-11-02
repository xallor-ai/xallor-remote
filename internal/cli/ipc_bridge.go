package cli

import (
	"github.com/spf13/cobra"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"io"
	"net"
	"os"
	"time"
)

// GUI transports share the Go profile/ensure contract. This is a byte bridge,
// not another dispatcher; one child owns exactly one Runtime IPC connection.
func cmdIPCBridge() *cobra.Command {
	return &cobra.Command{Use: "ipc-bridge", Hidden: true, Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := ensureRuntime(); err != nil {
				return err
			}
			c, err := ipc.Dial(2 * time.Second)
			if err != nil {
				return err
			}
			return bridgeIPC(c, os.Stdin, os.Stdout)
		},
	}
}

func bridgeIPC(c net.Conn, in io.Reader, out io.Writer) error {
	defer c.Close()
	done := make(chan error, 2)
	go func() { _, err := io.Copy(c, in); done <- err }()
	go func() { _, err := io.Copy(out, c); done <- err }()
	// Closing either head stream ends the IPC session and cancels its live execs.
	return <-done
}
